"""Binance WebSocket 約定板ストリーム。

Binance の部分深度ストリーム（<symbol>@depth<levels>）を購読し、最新の
orderbook スナップショットを保持する。detect-signal の入力や /api/v1/orderbook
のレスポンスに使う。

接続断は自動リトライする。接続状態・エラー履歴は system ページで確認できる
ように保持し、logger にも出力する。
"""

import asyncio
import json
import logging
import time
from datetime import datetime, timezone

import websockets

logger = logging.getLogger("crypto-api.binance")

DEFAULT_URL_TEMPLATE = "wss://stream.binance.com:9443/ws/{symbol}@depth{levels}"
MAX_ERROR_LOG = 20


def _iso_ms(ms: int) -> str:
    return datetime.fromtimestamp(ms / 1000, tz=timezone.utc).isoformat()


class BinanceStream:
    """Binance の depth stream を購読し、最新 orderbook を保持する。"""

    def __init__(
        self,
        symbol: str = "btcusdt",
        levels: int = 5,
        reconnect_delay: float = 2.0,
    ):
        self.symbol = symbol
        self.levels = levels
        self.url = DEFAULT_URL_TEMPLATE.format(symbol=symbol, levels=levels)
        self.reconnect_delay = reconnect_delay
        self._lock = asyncio.Lock()
        self._orderbook = None
        self._status = "connecting"
        self._messages = 0
        self._reconnects = 0
        self._last_update_ms = 0
        self._errors = []

    def _log(self, level: str, message: str) -> None:
        entry = {
            "timestamp": _iso_ms(int(time.time() * 1000)),
            "level": level,
            "message": message,
        }
        self._errors.append(entry)
        self._errors = self._errors[-MAX_ERROR_LOG:]
        log_fn = {
            "info": logger.info,
            "warn": logger.warning,
            "error": logger.error,
        }.get(level, logger.info)
        log_fn("binance ws: %s", message)

    def _parse_side(self, rows) -> list:
        """Binance の [["price","qty"], ...] を {"price": .., "volume": ..} の list に変換する。"""
        result = []
        if not rows:
            return result
        for row in rows:
            if not isinstance(row, (list, tuple)) or len(row) < 2:
                continue
            try:
                price = float(row[0])
                volume = float(row[1])
            except (TypeError, ValueError):
                continue
            if price > 0 and volume > 0:
                result.append({"price": price, "volume": volume})
        return result

    async def run(self) -> None:
        """接続ループ。切断時は自動リトライする。CancelledError で終了する。"""
        while True:
            try:
                self._status = "connecting"
                async with websockets.connect(
                    self.url,
                    ping_interval=20,
                    ping_timeout=20,
                    max_size=2**20,
                ) as ws:
                    self._status = "connected"
                    self._log("info", f"connected: {self.url}")
                    async for raw in ws:
                        try:
                            msg = json.loads(raw)
                        except json.JSONDecodeError:
                            continue
                        bids = self._parse_side(msg.get("bids"))
                        asks = self._parse_side(msg.get("asks"))
                        if not bids or not asks:
                            continue
                        orderbook = {
                            "symbol": self.symbol.upper(),
                            "bids": bids,
                            "asks": asks,
                            "timestamp": int(time.time() * 1000),
                            "mid_price": round(
                                (bids[0]["price"] + asks[0]["price"]) / 2, 2
                            ),
                            "spread": round(asks[0]["price"] - bids[0]["price"], 2),
                        }
                        async with self._lock:
                            self._orderbook = orderbook
                            self._messages += 1
                            self._last_update_ms = orderbook["timestamp"]
            except asyncio.CancelledError:
                raise
            except Exception as exc:  # noqa: BLE001
                self._status = "error"
                self._reconnects += 1
                self._log(
                    "error",
                    f"connection error: {exc}; reconnecting in "
                    f"{self.reconnect_delay:.0f}s (reconnect #{self._reconnects})",
                )
                try:
                    await asyncio.sleep(self.reconnect_delay)
                except asyncio.CancelledError:
                    raise

    async def latest(self) -> dict | None:
        """最新 orderbook スナップショット（未接続時は None）。"""
        async with self._lock:
            return self._orderbook

    def snapshot(self) -> dict:
        """接続状態・エラー履歴のスナップショット（system ページ用）。"""
        return {
            "status": self._status,
            "url": self.url,
            "symbol": self.symbol.upper(),
            "levels": self.levels,
            "messages": self._messages,
            "reconnects": self._reconnects,
            "last_update": (
                _iso_ms(self._last_update_ms) if self._last_update_ms else None
            ),
            "errors": list(self._errors),
        }
