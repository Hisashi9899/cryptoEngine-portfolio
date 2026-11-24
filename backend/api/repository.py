"""DB への読み書き。

DB 障害時は各関数が None を返し、呼び出し側（main.py）が demo データへ
フォールバックする。API レスポンス形式は demo_data.py と同一に保つ。
"""

import logging
import os
import sys
from datetime import datetime

from sqlalchemy import text

_SELF_DIR = os.path.dirname(os.path.abspath(__file__))
if _SELF_DIR not in sys.path:
    sys.path.insert(0, _SELF_DIR)

from db import SessionLocal  # noqa: E402

logger = logging.getLogger("crypto-api.repository")

INITIAL_CAPITAL = 1_000_000.0
AVAILABLE_RATIO = 0.62

_EMPTY = {
    "signals": "signals",
    "trades": "trades",
    "positions": "positions",
    "balance_history": "balance_history",
}


def _iso(ms: int) -> str:
    return datetime.fromtimestamp(ms / 1000).isoformat()


def _parse_ms(value) -> int:
    try:
        return int(datetime.fromisoformat(value).timestamp() * 1000)
    except (ValueError, TypeError):
        return 0


def _table_empty(session, table: str) -> bool:
    row = session.execute(text(f"SELECT COUNT(*) FROM {table}")).scalar()
    return not row


# --- seed（テーブルが空のときのみ demo データを投入） ----------------------


def seed_signals(session) -> None:
    if not _table_empty(session, "signals"):
        return
    from demo_data import get_signals

    for sig in get_signals()["signals"]:
        session.execute(
            text(
                """
                INSERT INTO signals (symbol, type, confidence, reason, mid_price, spread_basis, timestamp)
                VALUES (:symbol, :type, :confidence, :reason, :mid_price, :spread, :timestamp)
                """
            ),
            {
                "symbol": sig["symbol"],
                "type": sig["type"],
                "confidence": sig["confidence"],
                "reason": sig["reason"],
                "mid_price": sig["mid_price"],
                "spread": sig["spread"],
                "timestamp": _parse_ms(sig["timestamp"]),
            },
        )
    session.commit()


def seed_trades(session) -> None:
    if not _table_empty(session, "trades"):
        return
    from demo_data import get_trades

    for trade in get_trades()["trades"]:
        session.execute(
            text(
                """
                INSERT INTO trades (symbol, side, price, volume, pnl, timestamp, order_id, status)
                VALUES (:symbol, :side, :price, :volume, :pnl, :timestamp, :order_id, :status)
                """
            ),
            {
                "symbol": trade["symbol"],
                "side": trade["side"],
                "price": trade["price"],
                "volume": trade["volume"],
                "pnl": trade.get("pnl"),
                "timestamp": _parse_ms(trade["timestamp"]),
                "order_id": trade.get("id", ""),
                "status": "filled",
            },
        )
    session.commit()


def seed_positions(session) -> None:
    if not _table_empty(session, "positions"):
        return
    from demo_data import get_positions

    now = int(datetime.utcnow().timestamp() * 1000)
    for p in get_positions()["positions"]:
        session.execute(
            text(
                """
                INSERT INTO positions
                    (symbol, side, size, volume, avg_price, last_price, market_value,
                     unrealized_pnl, unrealized_pnl_rate, timestamp)
                VALUES
                    (:symbol, 'long', :size, :size, :avg_price, :last_price, :market_value,
                     :unrealized_pnl, :unrealized_pnl_rate, :timestamp)
                """
            ),
            {
                "symbol": p["symbol"],
                "size": p["size"],
                "avg_price": p["avg_price"],
                "last_price": p["last_price"],
                "market_value": p["market_value"],
                "unrealized_pnl": p["unrealized_pnl"],
                "unrealized_pnl_rate": p["unrealized_pnl_rate"],
                "timestamp": now,
            },
        )
    session.commit()


def seed_balance(session) -> None:
    if not _table_empty(session, "balance_history"):
        return
    from demo_data import get_balance

    for point in get_balance()["history"]:
        balance = point["balance"]
        session.execute(
            text(
                """
                INSERT INTO balance_history (asset, free, locked, timestamp)
                VALUES ('JPY', :free, :locked, :timestamp)
                """
            ),
            {
                "free": round(balance * AVAILABLE_RATIO, 2),
                "locked": round(balance * (1 - AVAILABLE_RATIO), 2),
                "timestamp": _parse_ms(point["timestamp"]),
            },
        )
    session.commit()


# --- read（API レスポンス形式は demo_data.py と同一） ----------------------


def read_balance(session) -> dict | None:
    rows = session.execute(
        text(
            """
            SELECT asset, free, locked, timestamp
            FROM balance_history
            WHERE asset = 'JPY'
            ORDER BY timestamp ASC
            """
        )
    ).fetchall()
    if not rows:
        return None

    latest = rows[-1]
    total = latest.free + latest.locked
    history = [
        {"timestamp": _iso(r.timestamp), "balance": round(r.free + r.locked, 2)}
        for r in rows
    ]
    return {
        "currency": "JPY",
        "total_assets": round(total, 2),
        "available": round(latest.free, 2),
        "invested": round(latest.locked, 2),
        "profit_loss": round(total - INITIAL_CAPITAL, 2),
        "profit_loss_rate": round((total - INITIAL_CAPITAL) / INITIAL_CAPITAL * 100, 2),
        "initial_capital": INITIAL_CAPITAL,
        "history": history,
    }


def read_trades(session, symbol: str | None = None, side: str | None = None, limit: int = 50) -> dict | None:
    where = ["1=1"]
    params = {"limit": limit}
    if symbol:
        where.append("symbol = :symbol")
        params["symbol"] = symbol
    if side:
        where.append("side = :side")
        params["side"] = side

    rows = session.execute(
        text(
            f"""
            SELECT id, symbol, side, price, volume, pnl, timestamp
            FROM trades
            WHERE {' AND '.join(where)}
            ORDER BY timestamp DESC
            LIMIT :limit
            """
        ),
        params,
    ).fetchall()
    if not rows:
        return None

    trades = [
        {
            "id": f"T{row.id:05d}",
            "timestamp": _iso(row.timestamp),
            "symbol": row.symbol,
            "side": row.side,
            "price": row.price,
            "volume": row.volume,
            "pnl": row.pnl,
        }
        for row in rows
    ]
    return {"trades": trades}


def read_positions(session) -> dict | None:
    rows = session.execute(
        text(
            """
            SELECT symbol, size, avg_price, last_price, market_value, unrealized_pnl, unrealized_pnl_rate
            FROM positions
            ORDER BY timestamp DESC
            """
        )
    ).fetchall()
    if not rows:
        return None

    positions = [
        {
            "symbol": row.symbol,
            "size": row.size,
            "avg_price": row.avg_price,
            "last_price": row.last_price,
            "market_value": row.market_value,
            "unrealized_pnl": row.unrealized_pnl,
            "unrealized_pnl_rate": row.unrealized_pnl_rate,
        }
        for row in rows
    ]
    total_pnl = sum(p["unrealized_pnl"] for p in positions)
    return {"positions": positions, "total_unrealized_pnl": round(total_pnl, 2)}


def read_signals(session, symbol: str | None = None, limit: int = 50) -> dict | None:
    where = ["1=1"]
    params = {"limit": limit}
    if symbol:
        where.append("symbol = :symbol")
        params["symbol"] = symbol

    rows = session.execute(
        text(
            f"""
            SELECT id, symbol, type, confidence, reason, mid_price, spread_basis, timestamp
            FROM signals
            WHERE {' AND '.join(where)}
            ORDER BY timestamp DESC
            LIMIT :limit
            """
        ),
        params,
    ).fetchall()
    if not rows:
        return None

    signals = [
        {
            "id": f"S{row.id:04d}",
            "timestamp": _iso(row.timestamp),
            "symbol": row.symbol,
            "type": row.type,
            "confidence": row.confidence,
            "reason": row.reason,
            "mid_price": row.mid_price,
            "spread": row.spread_basis,
        }
        for row in rows
    ]
    return {"signals": signals}


def read_trade_pairs(session, limit: int = 100) -> dict | None:
    """ペアリング済みトレード（trade_pairs）を直近から返す。"""
    rows = session.execute(
        text(
            """
            SELECT id, symbol, entry_side, exit_side, entry_price, exit_price,
                   entry_time, exit_time, entry_volume, exit_volume, pnl, duration_seconds
            FROM trade_pairs
            ORDER BY exit_time DESC
            LIMIT :limit
            """
        ),
        {"limit": limit},
    ).fetchall()
    if not rows:
        return None

    pairs = [
        {
            "id": f"P{row.id:05d}",
            "symbol": row.symbol,
            "entry_side": row.entry_side,
            "exit_side": row.exit_side,
            "entry_price": row.entry_price,
            "exit_price": row.exit_price,
            "entry_time": _iso(row.entry_time),
            "exit_time": _iso(row.exit_time),
            "entry_volume": row.entry_volume,
            "exit_volume": row.exit_volume,
            "pnl": row.pnl,
            "duration_seconds": row.duration_seconds,
        }
        for row in rows
    ]
    return {"trade_pairs": pairs}


# --- write ---------------------------------------------------------------


def save_signal(session, signal: dict) -> bool:
    """シグナルを DB に保存する。失敗しても例外を投げず False を返す。"""
    try:
        session.execute(
            text(
                """
                INSERT INTO signals (symbol, type, confidence, reason, mid_price, spread_basis, timestamp)
                VALUES (:symbol, :type, :confidence, :reason, :mid_price, :spread_basis, :timestamp)
                """
            ),
            {
                "symbol": signal.get("symbol", ""),
                "type": signal.get("type", "hold"),
                "confidence": float(signal.get("confidence", 0) or 0),
                "reason": signal.get("reason", ""),
                "mid_price": float(signal.get("mid_price", 0) or 0),
                "spread_basis": float(signal.get("spread_basis", 0) or 0),
                "timestamp": int(signal.get("timestamp", 0) or 0),
            },
        )
        session.commit()
        return True
    except Exception:
        logger.warning("failed to save signal", exc_info=True)
        return False
