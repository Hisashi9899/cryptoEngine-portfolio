"""gRPC クライアント。

cryptoEngine の Go core が公開する gRPC サーバー（engine.v1.Engine）への
クライアントを提供する。RPC が失敗した場合のフォールバック（subprocess 等）は
呼び出し側（main.py）で行う。
"""

import logging
import os
import sys

# engine_pb2_grpc.py 内部の `import engine_pb2`（絶対 import）がどの起動方法でも
# 解決されるよう、スタブが置かれているディレクトリを sys.path に追加する。
_GRPC_STUB_DIR = os.path.dirname(os.path.abspath(__file__))
if _GRPC_STUB_DIR not in sys.path:
    sys.path.insert(0, _GRPC_STUB_DIR)

import grpc  # noqa: E402

import engine_pb2 as pb  # noqa: E402
import engine_pb2_grpc as pb_grpc  # noqa: E402

logger = logging.getLogger("crypto-api.grpc")

DEFAULT_GRPC_TARGET = os.environ.get("CORE_GRPC_TARGET", "localhost:50051")
DEFAULT_TIMEOUT = 5.0
DEFAULT_SPREAD_THRESHOLD = 0.0001

# proto の SignalType enum -> API レスポンスで使う文字列
_SIGNAL_TYPE_TO_STR = {
    pb.SignalType.SIGNAL_TYPE_BUY: "buy",
    pb.SignalType.SIGNAL_TYPE_SELL: "sell",
    pb.SignalType.SIGNAL_TYPE_HOLD: "hold",
    pb.SignalType.SIGNAL_TYPE_UNSPECIFIED: "hold",
}


class GrpcClientError(Exception):
    """gRPC 呼び出しが失敗したことを表す例外。"""


def _to_orders(rows) -> list:
    """行データを proto の Order へ変換する。

    CLI（subprocess）互換の [["price","volume"], ...] 形式と、
    [{"price": .., "volume": ..}, ...] 形式の両方に対応する。
    """
    orders = []
    for row in rows:
        if isinstance(row, (list, tuple)) and len(row) >= 2:
            orders.append(pb.Order(price=float(row[0]), volume=float(row[1])))
        elif isinstance(row, dict) and "price" in row and "volume" in row:
            orders.append(pb.Order(price=float(row["price"]), volume=float(row["volume"])))
    return orders


def _order_book_to_proto(order_book: dict) -> pb.OrderBook:
    return pb.OrderBook(
        symbol=order_book.get("symbol", ""),
        bids=_to_orders(order_book.get("bids", [])),
        asks=_to_orders(order_book.get("asks", [])),
        timestamp=order_book.get("timestamp", 0),
        mid_price=float(order_book.get("mid_price", 0.0)),
        spread=float(order_book.get("spread", 0.0)),
    )


def _signal_to_dict(sig) -> dict:
    return {
        "symbol": sig.symbol,
        "type": _SIGNAL_TYPE_TO_STR.get(sig.type, "hold"),
        "confidence": sig.confidence,
        "reason": sig.reason,
        "mid_price": sig.mid_price,
        "spread_basis": sig.spread_basis,
        "timestamp": sig.timestamp,
    }


class GrpcClient:
    """engine.v1.Engine サービスへの同期クライアント。"""

    def __init__(self, target: str = DEFAULT_GRPC_TARGET, timeout: float = DEFAULT_TIMEOUT):
        self.target = target
        self.timeout = timeout
        self._channel = grpc.insecure_channel(target)
        self.stub = pb_grpc.EngineStub(self._channel)
        logger.info("gRPC client initialized: target=%s timeout=%.1fs", target, timeout)

    def close(self):
        self._channel.close()

    def fetch_orderbook(self, symbol: str, limit: int = 50) -> dict:
        """約定板データを取得し、subprocess と同一の dict 形式で返す。"""
        try:
            resp = self.stub.FetchOrderBook(
                pb.FetchOrderBookRequest(symbol=symbol, limit=int(limit)),
                timeout=self.timeout,
            )
        except grpc.RpcError as e:
            logger.warning("FetchOrderBook rpc failed: %s", e)
            raise GrpcClientError(f"FetchOrderBook failed: {e.details() or e}") from e

        ob = resp.order_book
        return {
            "order_book": {
                "symbol": ob.symbol,
                "bids": [{"price": o.price, "volume": o.volume} for o in ob.bids],
                "asks": [{"price": o.price, "volume": o.volume} for o in ob.asks],
                "timestamp": ob.timestamp,
                "mid_price": ob.mid_price,
                "spread": ob.spread,
            },
            "latency_ms": resp.latency_ms,
        }

    def fetch_ticker(self, symbol: str = "BTCUSDT") -> dict:
        """ティッカーを取得し、subprocess と同一の dict 形式で返す。"""
        try:
            resp = self.stub.FetchTicker(
                pb.FetchTickerRequest(symbol=symbol),
                timeout=self.timeout,
            )
        except grpc.RpcError as e:
            logger.warning("FetchTicker rpc failed: %s", e)
            raise GrpcClientError(f"FetchTicker failed: {e.details() or e}") from e

        tk = resp.ticker
        return {
            "ticker": {
                "symbol": tk.symbol,
                "last": tk.last,
                "bid": tk.bid,
                "ask": tk.ask,
                "volume": tk.volume,
            },
            "latency_ms": resp.latency_ms,
        }

    def detect_signal(
        self,
        order_book: dict,
        spread_threshold: float = DEFAULT_SPREAD_THRESHOLD,
    ) -> dict:
        """約定板データからシグナルを検出し、subprocess と同一の dict 形式で返す。"""
        try:
            resp = self.stub.DetectSignal(
                pb.DetectSignalRequest(
                    order_book=_order_book_to_proto(order_book),
                    spread_threshold=float(spread_threshold),
                ),
                timeout=self.timeout,
            )
        except grpc.RpcError as e:
            logger.warning("DetectSignal rpc failed: %s", e)
            raise GrpcClientError(f"DetectSignal failed: {e.details() or e}") from e

        return {"signal": _signal_to_dict(resp.signal)}
