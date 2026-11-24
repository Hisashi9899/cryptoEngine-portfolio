"""実資産エクイティ API（GMO口座連動）。

- GET /api/v1/equity/live  GMO残高×板midの時価総額＋履歴。発注なし。
- paper残高（/api/v1/balance・初期資金100万円）とは別物。
  ダッシュボードは LIVE/PAPER を切り替えて表示する。

GMO未設定・メンテ中は直近スナップショットを stale:true で返す。
スナップショットが1件も無ければ status:error を返す（数字の捏造はしない）。
"""

import json
import logging
import time
import urllib.request as _req
from datetime import datetime, timezone

from fastapi import APIRouter, Depends
from sqlalchemy import text

logger = logging.getLogger("crypto-api.equity_route")
router = APIRouter()

try:
    import db
    import equity_math
    import security
    from routes.auto_trade import _run_core_json
except ImportError:
    from .. import db
    from .. import equity_math
    from .. import security
    from .auto_trade import _run_core_json


_EQUITY_SCHEMA = """
CREATE TABLE IF NOT EXISTS equity_snapshots (
    ts_ms          BIGINT PRIMARY KEY,
    total_jpy      DOUBLE PRECISION NOT NULL DEFAULT 0,
    jpy_available  DOUBLE PRECISION NOT NULL DEFAULT 0
);
"""


def _ensure_table() -> bool:
    try:
        with db.SessionLocal() as session:
            session.execute(text(_EQUITY_SCHEMA))
            session.commit()
        return True
    except Exception as e:  # noqa: BLE001
        logger.warning("equity snapshots table unavailable (%s)", e)
        return False


def _fetch_gmo_last(symbol: str, timeout: int = 10) -> float:
    """GMO Public ティッカーの last（JPY）を返す。キー不要。失敗時は 0。"""
    try:
        req = _req.Request(
            f"https://api.coin.z.com/public/v1/ticker?symbol={symbol}",
            headers={"User-Agent": "cryptoEngine-api"},
        )
        with _req.urlopen(req, timeout=timeout) as resp:
            env = json.loads(resp.read().decode("utf-8"))
        rows = env.get("data") or []
        if not rows:
            return 0.0
        try:
            return float(rows[0].get("last", 0) or 0)
        except (TypeError, ValueError):
            return 0.0
    except Exception:  # noqa: BLE001
        return 0.0


def _save_snapshot(total: float, jpy_available: float) -> None:
    """5分以上の間隔が空いていれば保存し、90日より古い分を刈る。失敗は警告のみ。"""
    try:
        now_ms = int(time.time() * 1000)
        with db.SessionLocal() as session:
            last = session.execute(
                text("SELECT MAX(ts_ms) FROM equity_snapshots")
            ).scalar()
            if last and now_ms - int(last) < equity_math.SNAPSHOT_MIN_INTERVAL_SEC * 1000:
                return
            session.execute(
                text(
                    "INSERT INTO equity_snapshots (ts_ms, total_jpy, jpy_available) "
                    "VALUES (:ts, :total, :avail) "
                    "ON CONFLICT (ts_ms) DO UPDATE SET total_jpy = EXCLUDED.total_jpy, "
                    "jpy_available = EXCLUDED.jpy_available"
                ),
                {"ts": now_ms, "total": float(total), "avail": float(jpy_available)},
            )
            session.execute(
                text(
                    "DELETE FROM equity_snapshots WHERE ts_ms < :cutoff"
                ),
                {"cutoff": now_ms - equity_math.SNAPSHOT_RETENTION_DAYS * 86400 * 1000},
            )
            session.commit()
    except Exception as e:  # noqa: BLE001
        logger.warning("equity snapshot save failed (%s)", e)


def _read_history(limit: int = 500) -> list:
    try:
        with db.SessionLocal() as session:
            rows = session.execute(
                text(
                    "SELECT ts_ms, total_jpy FROM equity_snapshots "
                    "ORDER BY ts_ms ASC"
                )
            ).fetchall()
    except Exception as e:  # noqa: BLE001
        logger.warning("equity history read failed (%s)", e)
        return []
    return equity_math.shape_history(
        [(int(r[0]), float(r[1])) for r in rows], limit=limit
    )


def _stale_response(history: list) -> dict:
    """GMO取得失敗時：直近スナップショットを stale:true で返す。無ければ error。"""
    if not history:
        return {"status": "error", "message": "GMO残高を取得できず、履歴もありません"}
    last = history[-1]
    return {
        "status": "ok",
        "source": "gmo",
        "stale": True,
        "total_jpy": last["balance"],
        "jpy_available": None,
        "jpy_balance": None,
        "holdings": [],
        "updated_at": last["timestamp"],
        "pnl_24h": equity_math.pnl_vs_history(last["balance"], history),
        "history": history,
    }


@router.get("/api/v1/equity/live")
def equity_live(_: str = Depends(security.require_auth)):
    """GMO実資産の時価総額＋履歴。発注しない読み取り専用。"""
    _ensure_table()
    result = _run_core_json(["gmo-assets"])
    if not result or result.get("status") != "ok":
        return _stale_response(_read_history())
    assets = result.get("assets", [])
    symbols = sorted(
        {
            str(a.get("symbol", "") or "").upper()
            for a in (assets or [])
            if str(a.get("symbol", "") or "").upper() not in ("", "JPY")
        }
    )
    prices = {s: _fetch_gmo_last(s) for s in symbols}
    calc = equity_math.compute_equity(assets, prices)
    now = datetime.now(timezone.utc).isoformat()
    history = _read_history()
    _save_snapshot(calc["total_jpy"], calc["jpy_available"])
    # 保存直後の最新点を履歴に反映する（次回以降のポーリングでも拾われる）。
    history = _read_history()
    return {
        "status": "ok",
        "source": "gmo",
        "stale": False,
        "total_jpy": calc["total_jpy"],
        "jpy_available": calc["jpy_available"],
        "jpy_balance": calc["jpy_balance"],
        "holdings": calc["holdings"],
        "updated_at": now,
        "pnl_24h": equity_math.pnl_vs_history(calc["total_jpy"], history),
        "history": history,
    }
