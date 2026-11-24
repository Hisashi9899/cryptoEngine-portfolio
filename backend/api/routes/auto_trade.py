"""自動売買コントロール API。

- GET  /api/v1/auto-trade                現在の状態（ON/OFF・リスク・ポジション・ログ）
- POST /api/v1/auto-trade/toggle         自動売買の ON/OFF 切替
- GET  /api/v1/auto-trade/affordability  口座余力に対する銘柄別取引可否（発注なし）
- GET  /api/v1/gmo/balance               GMO実残高（読み取り専用、発注なし）

実行そのものは main.py のシグナルループから auto_trade.maybe_execute() が
core CLI（execute-order）を subprocess で呼んで行う。ここでは状態の表示と
操作のみを扱う。
"""

import json
import logging
import subprocess
from pathlib import Path

from fastapi import APIRouter, Body, Depends
from pydantic import BaseModel

logger = logging.getLogger("crypto-api.auto_trade_route")
router = APIRouter()

try:
    import auto_trade
    import db
    import repository
    import security
except ImportError:
    from .. import auto_trade
    from .. import db
    from .. import repository
    from .. import security


def _read_positions() -> list:
    """DB から現在ポジションを読む。volume>0のみ・symbol重複排除。失敗・空なら空リスト。"""
    try:
        with db.SessionLocal() as session:
            result = repository.read_positions(session)
            if result is None:
                return []
            seen: dict[str, dict] = {}
            for p in result.get("positions", []):
                try:
                    vol = float(p.get("size", 0) or p.get("volume", 0) or 0)
                except (TypeError, ValueError):
                    vol = 0
                if vol <= 0:
                    continue
                sym = p.get("symbol", "")
                if sym and sym not in seen:
                    seen[sym] = p
            return list(seen.values())
    except Exception as e:  # noqa: BLE001
        logger.warning("read positions failed (%s)", e)
    return []


class ToggleRequest(BaseModel):
    enabled: bool


@router.get("/api/v1/auto-trade")
def auto_trade_state(_: str = Depends(security.require_auth)):
    """自動売買の現在状態を返す（ON/OFF・リスク状況・ポジション・直近ログ）。"""
    logs = auto_trade.read_trade_log(limit=50)
    return {
        "enabled": auto_trade.is_enabled(),
        "risk_stopped": auto_trade.is_risk_stopped(),
        "risk": {
            "max_position_size_usdt": auto_trade.MAX_POSITION_SIZE_USDT,
            "max_daily_loss_usdt": auto_trade.MAX_DAILY_LOSS_USDT,
            "trade_size_usdt": auto_trade.TRADE_SIZE_USDT,
            "stop_on_loss": auto_trade.STOP_ON_LOSS,
            "daily_loss_usdt": auto_trade.daily_pnl_usdt(),
        },
        "positions": _read_positions(),
        "trade_log": logs.get("logs", []),
    }


@router.get("/api/v1/auto-trade/affordability")
def auto_trade_affordability(_: str = Depends(security.require_auth)):
    """口座余力に対する銘柄別の取引可否を返す（発注しない読み取り専用）。

    core の gmo-affordability を subprocess で実行する。core 未導入・
    GMO 未設定時は error を返す（ダッシュボードはその旨を表示する）。
    """
    return _run_core_json(["gmo-affordability"])


@router.post("/api/v1/auto-trade/toggle")
def toggle_auto_trade(
    req: ToggleRequest = Body(default=ToggleRequest(enabled=False)),
    _: str = Depends(security.require_auth),
):
    """自動売買を ON/OFF する。ON にすると risk 停止状態を解除する。"""
    auto_trade.set_enabled(req.enabled)
    logger.info("auto trade toggled: %s", "ON" if req.enabled else "OFF")
    return {
        "enabled": auto_trade.is_enabled(),
        "risk_stopped": auto_trade.is_risk_stopped(),
    }


def _parse_core_stdout(stdout: str) -> dict | None:
    """coreのstdoutからJSON部分を抜き出す。INFOログが混入しても復元できる。"""
    text = (stdout or "").strip()
    if not text:
        return None
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        pass
    start = text.find("{")
    end = text.rfind("}")
    if start >= 0 and end > start:
        try:
            return json.loads(text[start : end + 1])
        except json.JSONDecodeError:
            return None
    return None


def _run_core_json(args: list, timeout: int = 60) -> dict | None:
    """coreバイナリを実行しJSONを返す。失敗時はNoneとstderrを返す。"""
    import os

    binary = auto_trade._resolve_core_binary()
    if not binary or not Path(binary).exists():
        return {"status": "error", "message": "core binary not found"}
    # GMO系は読み取り専用キーで実行。キー未設定はcore側がエラーを返す。
    _ = os.environ.get("EXCHANGE_ID", "")
    try:
        proc = subprocess.run(
            [binary, *args],
            capture_output=True,
            text=True,
            timeout=timeout,
        )
    except subprocess.TimeoutExpired:
        logger.error("core %s timed out", args[0] if args else "?")
        return {"status": "error", "message": f"{args[0] if args else '?'} timed out"}
    if proc.returncode != 0:
        err = (proc.stderr.strip() or proc.stdout.strip())[-500:]
        logger.warning("core %s failed: %s", args[0] if args else "?", err)
        msg = err or f"{args[0] if args else '?'} failed (check GMO_API_KEY)"
        # キー未設定をUIで判別できるようメッセージを正規化する。
        if "GMO_API_KEY" in msg or "api key" in msg.lower():
            return {"status": "error", "message": "GMO_API_KEY未設定", "detail": err}
        return {"status": "error", "message": msg}
    parsed = _parse_core_stdout(proc.stdout)
    if parsed is None:
        logger.error("core %s output invalid: %s", args[0] if args else "?", proc.stdout[:200])
        return {"status": "error", "message": f"{args[0] if args else '?'} output invalid"}
    return parsed


@router.get("/api/v1/gmo/balance")
def gmo_balance(_: str = Depends(security.require_auth)):
    """GMO実残高を読み取り専用で返す（発注なし）。

    coreのgmo-assetsをsubprocessで実行する。GMO未設定時は
    {"status":"error","message":"GMO_API_KEY未設定"}を返す。
    ダッシュボードはpaper残高（/api/v1/balance）と分けて表示すること。
    """
    result = _run_core_json(["gmo-assets"])
    if result is None:
        return {"status": "error", "message": "gmo-assets failed"}
    if result.get("status") == "ok":
        assets = result.get("assets", [])
        jpy_available = 0.0
        for a in assets:
            try:
                if str(a.get("symbol", "")).upper() == "JPY":
                    jpy_available = float(a.get("available", 0) or 0)
                    break
            except (TypeError, ValueError):
                continue
        result["jpy_available"] = jpy_available
    return result
