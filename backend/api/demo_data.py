"""Demo data generator for the monitoring dashboard.

Returns deterministic, realistic sample data so the Next.js dashboard can be
developed and previewed without a live exchange connection or a database.

デモデータ生成は _DEMO_ENABLED フラグで無効化できる。無効時は各 get_* が
空データを返す（実データは Binance WS / DB から供給される）。
"""

import random
from datetime import datetime

# デモデータ生成を無効化するフラグ。
# 本番検証時は False にすることで、シグナル・約定・バランス等のデモ値を
# 出さない（削除はせず、空データを返すだけ）。
_DEMO_ENABLED = False

SYMBOLS = ["BTCUSDT", "ETHUSDT", "XRPUSDT", "SOLUSDT", "ADAUSDT"]

BASE_PRICES = {
    "BTCUSDT": 5100000.0,
    "ETHUSDT": 350000.0,
    "XRPUSDT": 85.0,
    "SOLUSDT": 28000.0,
    "ADAUSDT": 180.0,
}


def _empty(kind: str) -> dict:
    """デモ無効時の空レスポンス。各 get_* の返却形式と同一構造を保つ。"""
    if kind == "trades":
        return {"trades": []}
    if kind == "trade_pairs":
        return {"trade_pairs": []}
    if kind == "signals":
        return {"signals": []}
    if kind == "positions":
        return {"positions": [], "total_unrealized_pnl": 0}
    if kind == "orderbook":
        return {"symbol": "", "bids": [], "asks": [], "timestamp": 0, "mid_price": 0, "spread": 0}
    if kind == "balance":
        return {
            "currency": "JPY",
            "total_assets": 0,
            "available": 0,
            "invested": 0,
            "profit_loss": 0,
            "profit_loss_rate": 0,
            "initial_capital": 1000000.0,
            "history": [],
        }
    return {}


def _now_ms() -> int:
    return int(datetime.utcnow().timestamp() * 1000)


def _iso(ms: int) -> str:
    return datetime.fromtimestamp(ms / 1000).isoformat()


def get_balance() -> dict:
    if not _DEMO_ENABLED:
        return _empty("balance")
    total = 1258430.75
    initial = 1000000.0
    pnl = total - initial

    points = 48
    values = [initial]
    for _ in range(points - 1):
        values.append(values[-1] * (1 + random.uniform(-0.004, 0.011)))
    scale = total / values[-1]
    values = [v * scale for v in values]

    step = 48 * 60 * 60 * 1000 // (points - 1)
    start = _now_ms() - 48 * 60 * 60 * 1000
    history = [
        {"timestamp": _iso(start + i * step), "balance": round(values[i], 2)}
        for i in range(points)
    ]

    return {
        "currency": "JPY",
        "total_assets": total,
        "available": round(total * 0.62, 2),
        "invested": round(total * 0.38, 2),
        "profit_loss": round(pnl, 2),
        "profit_loss_rate": round(pnl / initial * 100, 2),
        "initial_capital": initial,
        "history": history,
    }


def get_trades() -> dict:
    if not _DEMO_ENABLED:
        return _empty("trades")
    sides = ["buy", "sell"]
    trades = []
    now = _now_ms()
    for i in range(60):
        ts = now - random.randint(0, 48) * 3600_000 - random.randint(0, 3600_000)
        symbol = random.choice(SYMBOLS)
        base = BASE_PRICES[symbol]
        side = random.choice(sides)
        price = round(base * (1 + random.uniform(-0.004, 0.004)), 2)
        if symbol in ("BTCUSDT", "ETHUSDT", "SOLUSDT"):
            volume = round(random.uniform(0.001, 0.6), 6)
        else:
            volume = round(random.uniform(10, 2000), 2)
        pnl = round(random.uniform(-3000, 5200), 2) if side == "sell" else None
        trades.append(
            {
                "id": f"T{i:05d}",
                "timestamp": _iso(ts),
                "symbol": symbol,
                "side": side,
                "price": price,
                "volume": volume,
                "pnl": pnl,
            }
        )
    trades.sort(key=lambda t: t["timestamp"], reverse=True)
    return {"trades": trades}


def get_trade_pairs() -> dict:
    if not _DEMO_ENABLED:
        return _empty("trade_pairs")
    return {"trade_pairs": []}


def get_positions() -> dict:
    if not _DEMO_ENABLED:
        return _empty("positions")
    positions = [
        {"symbol": "BTCUSDT", "size": 0.045, "avg_price": 5020000.0},
        {"symbol": "ETHUSDT", "size": 2.4, "avg_price": 342000.0},
        {"symbol": "SOLUSDT", "size": 35, "avg_price": 26500.0},
    ]
    result = []
    for p in positions:
        last = BASE_PRICES[p["symbol"]] * (1 + random.uniform(-0.015, 0.015))
        market_value = last * p["size"]
        cost = p["avg_price"] * p["size"]
        upnl = market_value - cost
        result.append(
            {
                "symbol": p["symbol"],
                "size": p["size"],
                "avg_price": p["avg_price"],
                "last_price": round(last, 2),
                "market_value": round(market_value, 2),
                "unrealized_pnl": round(upnl, 2),
                "unrealized_pnl_rate": round(upnl / cost * 100, 2),
            }
        )
    total_pnl = sum(p["unrealized_pnl"] for p in result)
    return {"positions": result, "total_unrealized_pnl": round(total_pnl, 2)}


def get_signals() -> dict:
    if not _DEMO_ENABLED:
        return _empty("signals")
    reasons = {
        "buy": "buy pressure (imbalance=0.45 spread=0.0001)",
        "sell": "sell pressure (imbalance=-0.38 spread=0.0001)",
        "hold": "no clear signal (imbalance=0.05 spread=0.0001)",
    }
    signals = []
    now = _now_ms()
    for i in range(40):
        ts = now - random.randint(0, 24) * 3600_000 - random.randint(0, 3600_000)
        symbol = random.choice(SYMBOLS)
        stype = random.choices(["buy", "sell", "hold"], weights=[0.3, 0.3, 0.4])[0]
        mid = BASE_PRICES[symbol] * (1 + random.uniform(-0.003, 0.003))
        spread = round(random.uniform(5, 25) * 1e-5, 6)
        confidence = (
            random.uniform(0.3, 0.98) if stype != "hold" else random.uniform(0, 0.25)
        )
        signals.append(
            {
                "id": f"S{i:04d}",
                "timestamp": _iso(ts),
                "symbol": symbol,
                "type": stype,
                "confidence": round(confidence, 2),
                "reason": reasons[stype],
                "mid_price": round(mid, 2),
                "spread": spread,
            }
        )
    signals.sort(key=lambda s: s["timestamp"], reverse=True)
    return {"signals": signals}


def get_orderbook(symbol: str = "BTCUSDT", limit: int = 20) -> dict:
    if not _DEMO_ENABLED:
        return _empty("orderbook")
    mid = BASE_PRICES.get(symbol, 5000000.0) * (1 + random.uniform(-0.001, 0.001))
    tick = max(mid * 0.00005, 0.01)

    bids = []
    price = mid - tick
    for _ in range(limit):
        bids.append(
            {"price": round(price, 2), "volume": round(random.uniform(0.01, 1.5), 6)}
        )
        price -= tick * random.uniform(0.5, 1.5)

    asks = []
    price = mid + tick
    for _ in range(limit):
        asks.append(
            {"price": round(price, 2), "volume": round(random.uniform(0.01, 1.5), 6)}
        )
        price += tick * random.uniform(0.5, 1.5)

    best_bid = bids[0]["price"]
    best_ask = asks[0]["price"]
    return {
        "symbol": symbol,
        "bids": bids,
        "asks": asks,
        "timestamp": _now_ms(),
        "mid_price": round((best_bid + best_ask) / 2, 2),
        "spread": round(best_ask - best_bid, 2),
    }


def get_system() -> dict:
    now = _now_ms()
    return {
        "core": {
            "status": "running",
            "version": "0.1.0",
            "uptime_sec": 86400 - random.randint(0, 3600),
            "last_heartbeat": _iso(now - random.randint(1, 30) * 1000),
        },
        "api": {
            "status": "ok",
            "latency_ms": random.randint(4, 18),
            "requests_1h": random.randint(1200, 2400),
        },
        "docker": {
            "containers": [
                {
                    "name": "cryptoengine-api-1",
                    "image": "cryptoengine-api:latest",
                    "status": "running",
                    "uptime": "3d 4h",
                },
                {
                    "name": "cryptoengine-core-1",
                    "image": "cryptoengine-core:latest",
                    "status": "running",
                    "uptime": "3d 4h",
                },
                {
                    "name": "cryptoengine-trader-1",
                    "image": "cryptoengine-core:latest",
                    "status": "running",
                    "uptime": "3d 4h",
                },
                {
                    "name": "cryptoengine-frontend-1",
                    "image": "cryptoengine-frontend:latest",
                    "status": "running",
                    "uptime": "3d 4h",
                },
                {
                    "name": "cryptoengine-db-1",
                    "image": "postgres:16-alpine",
                    "status": "running",
                    "uptime": "3d 4h",
                },
            ]
        },
        "errors": [
            {
                "timestamp": _iso(now - 40 * 60 * 1000),
                "level": "error",
                "message": "orderbook fetch timeout: unexpected status 429",
            },
            {
                "timestamp": _iso(now - 3 * 3600 * 1000),
                "level": "warn",
                "message": "detect-signal: insufficient order book data",
            },
            {
                "timestamp": _iso(now - 26 * 3600 * 1000),
                "level": "error",
                "message": "ticker unmarshal failed: invalid field 'volume'",
            },
        ],
    }
