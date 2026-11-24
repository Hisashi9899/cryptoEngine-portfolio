"""BUY→SELL シグナルのペアリング（エントリー→クローズ）ロジック。

シンプルな「シグナルベース」のモデル:
- BUY シグナルが来たら、その symbol の「未クローズエントリー」を記録する。
- 同一 symbol で次の SELL シグナルが来たらペアリングして trade_pairs に保存し、
  未クローズエントリーをクリアする。
- エントリーが無い状態で SELL が来た場合はペアリング対象外として WARN ログを出す。

エントリー状態は open_entries テーブルで永続化する（API 再起動でも維持）。
シグナルには volume 情報が無いため、現状は固定値（PAIRING_VOLUME）を使う。
pnl は既存 UI の円表記（fmtYen）と整合させるため JPY で保存する。
"""

import logging
import os

from sqlalchemy import text

import db

logger = logging.getLogger("crypto-api.pairing")

# シグナルには volume が無いため固定値を使う（③で拡張する際に置き換え可能）。
PAIRING_VOLUME = float(os.environ.get("CRYPTOENGINE_PAIRING_VOLUME", "0.001"))
# pnl を JPY で保存するための換算レート（placeholder。後で実レートに差し替え可能）。
JPY_PER_USDT = float(os.environ.get("CRYPTOENGINE_JPY_PER_USDT", "150"))


def _jpy_per_unit(symbol: str) -> float:
    """v2/GMO統一: GMO現物（BTC等）は価格自体がJPYのため換算不要（×1）。
    Binance形式（BTCUSDT等）はUSDT建てのためJPY換算（×150既定）を適用する。
    """
    s = (symbol or "").strip().upper()
    if s.endswith("USDT"):
        return JPY_PER_USDT
    return 1.0

_ENTRY_SIDE = "BUY"
_EXIT_SIDE = "SELL"


def _find_open_entry(session, symbol: str) -> dict | None:
    row = session.execute(
        text(
            """
            SELECT symbol, side, price, volume, timestamp
            FROM open_entries
            WHERE symbol = :symbol
            """
        ),
        {"symbol": symbol},
    ).fetchone()
    if row is None:
        return None
    return {
        "symbol": row.symbol,
        "side": row.side,
        "price": row.price,
        "volume": row.volume,
        "timestamp": row.timestamp,
    }


def _upsert_open_entry(session, entry: dict) -> None:
    session.execute(
        text(
            """
            INSERT INTO open_entries (symbol, side, price, volume, timestamp)
            VALUES (:symbol, :side, :price, :volume, :timestamp)
            ON CONFLICT (symbol) DO UPDATE SET
                side = EXCLUDED.side,
                price = EXCLUDED.price,
                volume = EXCLUDED.volume,
                timestamp = EXCLUDED.timestamp,
                updated_at = now()
            """
        ),
        {
            "symbol": entry["symbol"],
            "side": entry["side"],
            "price": entry["price"],
            "volume": entry["volume"],
            "timestamp": entry["timestamp"],
        },
    )


def _insert_trade_pair(session, pair: dict) -> None:
    session.execute(
        text(
            """
            INSERT INTO trade_pairs
                (symbol, entry_side, exit_side, entry_price, exit_price,
                 entry_time, exit_time, entry_volume, exit_volume, pnl, duration_seconds)
            VALUES
                (:symbol, :entry_side, :exit_side, :entry_price, :exit_price,
                 :entry_time, :exit_time, :entry_volume, :exit_volume, :pnl, :duration_seconds)
            """
        ),
        {
            "symbol": pair["symbol"],
            "entry_side": pair["entry_side"],
            "exit_side": pair["exit_side"],
            "entry_price": pair["entry_price"],
            "exit_price": pair["exit_price"],
            "entry_time": pair["entry_time"],
            "exit_time": pair["exit_time"],
            "entry_volume": pair["entry_volume"],
            "exit_volume": pair["exit_volume"],
            "pnl": pair["pnl"],
            "duration_seconds": pair["duration_seconds"],
        },
    )


def process_signal(signal: dict) -> dict | None:
    """1 つのシグナルをペアリング状態に反映する。

    BUY は未クローズエントリーとして記録し、SELL は未クローズエントリーと
    ペアリングして trade_pairs に保存する。保存に成功したペアを返し、
    対象外（HOLD / エントリー無しの SELL）や失敗時は None を返す。
    """
    signal_type = (signal.get("type") or "hold").lower()
    symbol = signal.get("symbol", "")
    if not symbol or signal_type not in ("buy", "sell"):
        return None

    price = float(signal.get("mid_price") or 0)
    timestamp = int(signal.get("timestamp") or 0)

    try:
        with db.SessionLocal() as session:
            if signal_type == "buy":
                _upsert_open_entry(
                    session,
                    {
                        "symbol": symbol,
                        "side": _ENTRY_SIDE,
                        "price": price,
                        "volume": PAIRING_VOLUME,
                        "timestamp": timestamp,
                    },
                )
                session.commit()
                return None

            entry = _find_open_entry(session, symbol)
            if entry is None:
                logger.warning(
                    "SELL signal for %s ignored: no open entry (BUY not seen yet)",
                    symbol,
                )
                return None

            duration_seconds = max(0.0, (timestamp - entry["timestamp"]) / 1000.0)
            pnl = (price - entry["price"]) * entry["volume"] * _jpy_per_unit(symbol)
            pair = {
                "symbol": symbol,
                "entry_side": entry["side"],
                "exit_side": _EXIT_SIDE,
                "entry_price": entry["price"],
                "exit_price": price,
                "entry_time": entry["timestamp"],
                "exit_time": timestamp,
                "entry_volume": entry["volume"],
                "exit_volume": entry["volume"],
                "pnl": round(pnl, 2),
                "duration_seconds": round(duration_seconds, 3),
            }
            _insert_trade_pair(session, pair)
            session.execute(
                text("DELETE FROM open_entries WHERE symbol = :symbol"),
                {"symbol": symbol},
            )
            session.commit()
            logger.info(
                "paired %s BUY@%.2f -> SELL@%.2f pnl=%.2f JPY duration=%.1fs",
                symbol,
                pair["entry_price"],
                pair["exit_price"],
                pair["pnl"],
                pair["duration_seconds"],
            )
            return pair
    except Exception as exc:  # noqa: BLE001
        logger.warning("pairing failed for %s signal (%s)", signal_type, exc)
        return None
