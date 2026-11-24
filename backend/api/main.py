import asyncio
import json
import logging
import os
import subprocess
import sys
import time
from contextlib import asynccontextmanager
from pathlib import Path
from typing import Optional

from fastapi import Body, Depends, FastAPI, HTTPException, Query
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel

_SELF_DIR = os.path.dirname(os.path.abspath(__file__))
if _SELF_DIR not in sys.path:
    sys.path.insert(0, _SELF_DIR)

try:
    import db
    import repository
except ImportError:
    from . import db
    from . import repository

try:
    from grpc_client import GrpcClient
except ImportError:
    from .grpc_client import GrpcClient

try:
    from demo_data import (
        get_balance,
        get_orderbook as get_demo_orderbook,
        get_positions,
        get_signals,
        get_system,
        get_trades,
        get_trade_pairs as demo_get_trade_pairs,
    )
except ImportError:
    from .demo_data import (
        get_balance,
        get_orderbook as get_demo_orderbook,
        get_positions,
        get_signals,
        get_system,
        get_trades,
        get_trade_pairs as demo_get_trade_pairs,
    )

try:
    from binance_ws import BinanceStream
except ImportError:
    from .binance_ws import BinanceStream

try:
    from pairing import process_signal
except ImportError:
    from .pairing import process_signal

try:
    from routes import backtest
except ImportError:
    from .routes import backtest

try:
    from routes import local_verify
except ImportError:
    from .routes import local_verify

try:
    from routes import auto_trade as auto_trade_route
    from routes import equity as equity_route
    from routes import funding as funding_route
    from routes import grid as grid_route
    from routes import vt as vt_route
except ImportError:
    from .routes import auto_trade as auto_trade_route
    from .routes import equity as equity_route
    from .routes import funding as funding_route
    from .routes import grid as grid_route
    from .routes import vt as vt_route
try:
    from routes import auth as auth_route
except ImportError:
    from .routes import auth as auth_route

try:
    import auto_trade as auto_trade_service
except ImportError:
    from . import auto_trade as auto_trade_service

try:
    import security
except ImportError:
    from . import security

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
)
logger = logging.getLogger("crypto-api")

@asynccontextmanager
async def lifespan(app: FastAPI):
    # AUTH_SECRET 未設定は起動時点で失敗させる（fail-fast）。
    # 開発時のみ AUTH_ALLOW_DEV_SECRET=1 で既定シークレットの使用を許可する。
    try:
        security._secret()
    except RuntimeError as exc:
        logger.error("%s", exc)
        raise
    # APP_ENV=prod 時のみ DB パスワード強度を検証（dev では何もしない）。
    try:
        db.validate_prod_config()
    except RuntimeError as exc:
        logger.error("%s", exc)
        raise
    db.init_db()
    db.init_pairing_tables()
    binance_task = asyncio.create_task(binance.run())
    signal_task = asyncio.create_task(_signal_loop())
    pairing_task = asyncio.create_task(_pairing_loop())
    logger.info("started binance ws stream and signal/pairing loops")
    yield
    await local_verify.shutdown()
    signal_task.cancel()
    pairing_task.cancel()
    binance_task.cancel()
    for task in (signal_task, pairing_task, binance_task):
        try:
            await task
        except asyncio.CancelledError:
            pass
        except Exception:
            logger.warning("background task stopped with error", exc_info=True)
    logger.info("stopped binance ws stream and signal/pairing loops")


app = FastAPI(
    title="CryptoEngine API",
    description="暗号資産自動運用バックエンドAPI",
    version="0.1.0",
    lifespan=lifespan,
)

app.include_router(backtest.router)
app.include_router(local_verify.router)
app.include_router(auto_trade_route.router)
app.include_router(equity_route.router)
app.include_router(funding_route.router)
app.include_router(grid_route.router)
app.include_router(vt_route.router)
app.include_router(auth_route.router)

app.add_middleware(
    CORSMiddleware,
    allow_origins=[
        "http://localhost:3000",
        "http://127.0.0.1:3000",
    ],
    allow_methods=["*"],
    allow_headers=["*"],
    allow_credentials=True,
)


@app.middleware("http")
async def forwarded_proto(request, call_next):
    """リバースプロキシ（nginx 等）経由の HTTPS を認識する。

    X-Forwarded-Proto: https が来たらリクエストスキームを https として扱う。
    """
    fwd = request.headers.get("x-forwarded-proto")
    if fwd:
        request.scope["scheme"] = fwd.split(",")[0].strip()
    return await call_next(request)

CORE_BIN = Path(__file__).resolve().parent.parent / "core" / "core"

GRPC_TARGET = os.environ.get("CORE_GRPC_TARGET", "localhost:50051")
grpc_client = GrpcClient(target=GRPC_TARGET)

BINANCE_SYMBOL = os.environ.get("BINANCE_SYMBOL", "btcusdt")
BINANCE_DEPTH_LEVELS = int(os.environ.get("BINANCE_DEPTH_LEVELS", "5"))
SIGNAL_LOOP_INTERVAL_SEC = float(os.environ.get("SIGNAL_LOOP_INTERVAL_SEC", "1"))

binance = BinanceStream(symbol=BINANCE_SYMBOL, levels=BINANCE_DEPTH_LEVELS)
_latest_signal = None
_signal_lock = asyncio.Lock()


def _call_core_grpc(fn, *args, **kwargs):
    """gRPC 呼び出しを試みる。失敗時は None を返し、フォールバックに委ねる"""
    try:
        return fn(*args, **kwargs)
    except Exception as e:
        logger.warning("gRPC call failed (%s); falling back to subprocess", e)
        return None


def _detect_from_core(order_book: dict) -> dict | None:
    """orderbook を core の DetectSignal に渡し、シグナル dict を返す。失敗時 None。"""
    data = _call_core_grpc(grpc_client.detect_signal, order_book)
    if data is None or not data.get("signal"):
        logger.warning("detect-signal via gRPC failed; skipping")
        return None
    return data


EXCHANGE_ID = os.environ.get("EXCHANGE_ID", "binance").strip().lower()
_gmo_loop_warned = False


async def _signal_loop() -> None:
    """1 秒間隔で Binance の最新 orderbook からシグナルを検出し保持する（v1）。

    シグナルの永続化は core 側（DetectSignal 内の persistSignal）が行うため、
    ここでは保存しない（API 側で保存すると二重に INSERT される）。

    v2/GMO統一仕様: EXCHANGE_ID=gmo の場合、v1ループ全体を停止する。
    v2シグナルはtrader（core auto-loop）がDBへ直接永続化し、
    ペアリングは_pairing_loopが担当する。混在防止のためv1の生成・保持・
    ペアリング・発注をすべて行わない。
    """
    global _gmo_loop_warned
    if EXCHANGE_ID == "gmo":
        logger.info("v1 signal loop disabled (EXCHANGE_ID=gmo); v2 trader is the sole signal source")
        while True:
            await asyncio.sleep(60)
    while True:
        try:
            order_book = await binance.latest()
            if order_book is not None:
                data = await asyncio.to_thread(_detect_from_core, order_book)
                if data is not None:
                    signal = data["signal"]
                    global _latest_signal
                    async with _signal_lock:
                        _latest_signal = signal
                    await asyncio.to_thread(process_signal, signal)
                    await asyncio.to_thread(auto_trade_service.maybe_execute, signal)
        except Exception as e:
            logger.error("signal loop iteration failed: %s", e)
        await asyncio.sleep(SIGNAL_LOOP_INTERVAL_SEC)


_pair_last_id: int = 0


def _pairing_loop_interval() -> float:
    try:
        return max(2.0, float(os.environ.get("PAIRING_LOOP_INTERVAL_SEC", "5")))
    except (TypeError, ValueError):
        return 5.0


async def _pairing_loop() -> None:
    """DBの新規シグナルをペアリングする（v2/GMO用）。

    traderが書き込んだv2シグナル（BUY/SELL）をid順に取得し、
    process_signalでopen_entries/trade_pairsへ反映する。
    起動時に既存最大idから開始するため、過去の再処理は行わない。
    v1混在防止のため、gmo時はこのループが唯一のペアリング経路になる。
    """
    global _pair_last_id
    try:
        with db.SessionLocal() as session:
            from sqlalchemy import text as _text

            row = session.execute(_text("SELECT COALESCE(MAX(id), 0) FROM signals")).scalar()
            _pair_last_id = int(row or 0)
            logger.info("pairing loop starts from signal id > %d", _pair_last_id)
    except Exception as e:
        logger.warning("pairing loop init failed (%s); starting from 0", e)
        _pair_last_id = 0
    while True:
        try:
            await asyncio.to_thread(_pair_new_signals)
        except Exception as e:
            logger.error("pairing loop iteration failed: %s", e)
        await asyncio.sleep(_pairing_loop_interval())


def _pair_new_signals() -> None:
    """未処理シグナルをid順にペアリングする（同期処理・スレッド内実行用）。"""
    global _pair_last_id
    try:
        with db.SessionLocal() as session:
            from sqlalchemy import text as _text

            rows = session.execute(
                _text(
                    "SELECT id, symbol, type, confidence, reason, mid_price, spread_basis, timestamp "
                    "FROM signals WHERE id > :last ORDER BY id ASC LIMIT 200"
                ),
                {"last": _pair_last_id},
            ).fetchall()
            for row in rows:
                signal = {
                    "symbol": row.symbol,
                    "type": row.type,
                    "confidence": row.confidence,
                    "reason": row.reason,
                    "mid_price": row.mid_price,
                    "spread_basis": row.spread_basis,
                    "timestamp": row.timestamp,
                }
                try:
                    process_signal(signal)
                except Exception as e:
                    logger.warning("pairing failed for signal id %s (%s)", row.id, e)
                _pair_last_id = max(_pair_last_id, int(row.id))
    except Exception as e:
        logger.warning("pair signals read failed (%s)", e)


def _iso_ms(ms: int) -> str:
    from datetime import datetime, timezone

    return datetime.fromtimestamp(ms / 1000, tz=timezone.utc).isoformat()


def _read_trader_heartbeat() -> dict:
    """traderコンテナの生存記録を読む。無ければstoppedを返す。"""
    import json as _json

    candidates = [
        os.environ.get("TRADER_HEARTBEAT_PATH", ""),
        "/app/backend/core/trade_logs/trader_heartbeat.json",
        "backend/core/trade_logs/trader_heartbeat.json",
        str(Path(__file__).resolve().parent.parent / "core" / "trade_logs" / "trader_heartbeat.json"),
    ]
    now_ms = int(time.time() * 1000)
    for path in candidates:
        if not path:
            continue
        try:
            with open(path, encoding="utf-8") as f:
                hb = _json.load(f)
            ts = int(hb.get("ts", 0) or 0)
            age_s = max(0, (now_ms - ts) // 1000) if ts else -1
            return {
                "status": "running" if 0 <= age_s <= 120 else "stale",
                "ticks": int(hb.get("ticks", 0) or 0),
                "orders": int(hb.get("orders", 0) or 0),
                "last_signal": str(hb.get("last_signal", "") or ""),
                "age_sec": age_s,
            }
        except (OSError, ValueError):
            continue
    return {"status": "stopped", "ticks": 0, "orders": 0, "last_signal": "", "age_sec": -1}


def _build_system() -> dict:
    """system レスポンス。Binance WS の接続状況・エラー履歴を errors に反映する。"""
    system = get_system()
    snap = binance.snapshot()
    now_ms = int(time.time() * 1000)
    errors = list(snap["errors"])
    if snap["status"] == "connected":
        errors.append(
            {
                "timestamp": snap["last_update"] or _iso_ms(now_ms),
                "level": "info",
                "message": (
                    f"binance ws {snap['symbol']}@depth{snap['levels']} connected: "
                    f"{snap['messages']} msgs received"
                ),
            }
        )
    else:
        errors.append(
            {
                "timestamp": snap["last_update"] or _iso_ms(now_ms),
                "level": "warn",
                "message": f"binance ws status: {snap['status']} ({snap['url']})",
            }
        )
    system["errors"] = errors
    system["trader"] = _read_trader_heartbeat()
    return system


def _db_or_demo(seed, reader, demo, **kwargs):
    """DB 読み取りを試み、失敗・空の場合は demo データへフォールバックする。"""
    try:
        with db.SessionLocal() as session:
            seed(session)
            result = reader(session, **kwargs)
            if result is not None:
                return result
    except Exception as e:
        logger.warning("DB read failed (%s); falling back to demo data", e)
    return demo()


def _read_positions_real() -> dict:
    """実ポジションのみ返す（volume>0・symbol重複排除）。空でも空リストを返す。"""
    try:
        with db.SessionLocal() as session:
            result = repository.read_positions(session)
    except Exception as e:
        logger.warning("positions DB read failed (%s)", e)
        return {"positions": [], "total_unrealized_pnl": 0}
    if not result:
        return {"positions": [], "total_unrealized_pnl": 0}
    seen: dict[str, dict] = {}
    for p in result.get("positions", []):
        try:
            vol = float(p.get("size", 0) or 0)
            if vol == 0:
                vol = float(p.get("volume", 0) or 0)
        except (TypeError, ValueError):
            vol = 0
        if vol <= 0:
            continue
        sym = p.get("symbol", "")
        if sym and sym not in seen:
            seen[sym] = p
    positions = list(seen.values())
    total = round(sum(float(p.get("unrealized_pnl", 0) or 0) for p in positions), 2)
    return {"positions": positions, "total_unrealized_pnl": total}


def _read_trade_pairs_real(limit: int = 500) -> list:
    """trade_pairs を直近から返す。DB 空・失敗時は空リスト。"""
    try:
        with db.SessionLocal() as session:
            result = repository.read_trade_pairs(session, limit=limit)
    except Exception as e:
        logger.warning("trade-pairs DB read failed (%s)", e)
        return []
    if not result:
        return []
    return result.get("trade_pairs", [])


def _trades_from_sources(
    symbol: str | None = None, side: str | None = None, limit: int = 50
) -> dict:
    """Trade History（約定単位）の実データ。

    優先順位: DB trades → trade_log.csv（core の実注文）→ 空。
    DB trades / trade_log のどちらも無い場合は空リストを返す
    （demo フォールバックしない＝本番の空は正しく空表示）。
    """
    try:
        with db.SessionLocal() as session:
            repository.seed_trades(session)
            db_result = repository.read_trades(session, symbol=symbol, side=side, limit=limit)
            if db_result is not None:
                return db_result
    except Exception as e:
        logger.warning("trades DB read failed (%s)", e)
    try:
        logs = auto_trade_service.read_trade_log(limit=500).get("logs", [])
    except Exception as e:
        logger.warning("trade log read failed (%s)", e)
        logs = []
    trades = []
    for i, row in enumerate(logs):
        action = (row.get("action") or "").lower()
        if action not in ("buy", "sell"):
            continue
        sym = row.get("symbol", "")
        if symbol and sym != symbol:
            continue
        if side and action != side.lower():
            continue
        price = row.get("price") or 0
        vol = row.get("amount") or 0
        try:
            price_f = float(price or 0)
            vol_f = float(vol or 0)
        except (TypeError, ValueError):
            continue
        if price_f <= 0:
            continue
        pnl = row.get("pnl")
        try:
            pnl_f = float(pnl) if pnl not in (None, "") else None
        except (TypeError, ValueError):
            pnl_f = None
        trades.append(
            {
                "id": row.get("order_id") or f"L{i:05d}",
                "timestamp": row.get("timestamp", ""),
                "symbol": sym,
                "side": action,
                "price": price_f,
                "volume": vol_f,
                "pnl": pnl_f,
            }
        )
        if len(trades) >= limit:
            break
    return {"trades": trades[:limit]}


def _balance_from_real() -> dict:
    """残高の実データ。

    balance_history があればそれを優先し、無ければ
    初期資金＋trade_pairs 確定損益＋ポジション含み損益から算出する。
    DB 空でも総資産 0 円にはしない（初期資金を返す）。
    """
    initial = repository.INITIAL_CAPITAL
    try:
        with db.SessionLocal() as session:
            stored = repository.read_balance(session)
            if stored is not None and (stored.get("history") or stored.get("total_assets")):
                return stored
    except Exception as e:
        logger.warning("balance DB read failed (%s)", e)
    pairs = _read_trade_pairs_real(limit=500)
    pos = _read_positions_real()
    invested = round(sum(float(p.get("market_value", 0) or 0) for p in pos.get("positions", [])), 2)
    unrealized = float(pos.get("total_unrealized_pnl", 0) or 0)
    realized = round(sum(float(p.get("pnl", 0) or 0) for p in pairs), 2)
    total = round(initial + realized + unrealized, 2)
    available = round(total - invested, 2)
    history = []
    if pairs:
        ordered = sorted(pairs, key=lambda p: p.get("exit_time", ""))
        cum = initial
        for p in ordered:
            try:
                cum += float(p.get("pnl", 0) or 0)
            except (TypeError, ValueError):
                continue
            history.append({"timestamp": p.get("exit_time", ""), "balance": round(cum, 2)})
        history = history[-200:]
    pnl = round(total - initial, 2)
    rate = round(pnl / initial * 100, 2) if initial else 0
    return {
        "currency": "JPY",
        "total_assets": total,
        "available": available,
        "invested": invested,
        "profit_loss": pnl,
        "profit_loss_rate": rate,
        "initial_capital": initial,
        "history": history,
    }


def _run_core(*args: str) -> dict:
    cmd = [str(CORE_BIN), *args]
    logger.info("Executing: %s", " ".join(cmd))
    start = time.time()

    result = subprocess.run(
        cmd,
        capture_output=True,
        text=True,
        timeout=30,
    )

    elapsed = time.time() - start
    logger.info("Completed in %.3fs (rc=%d)", elapsed, result.returncode)

    if result.returncode != 0:
        stderr = (result.stderr.strip() or result.stdout.strip())[-500:]
        logger.error("Core stderr: %s", stderr)
        raise HTTPException(status_code=502, detail=stderr)

    text = (result.stdout or "").strip()
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
            pass
    logger.error("JSON parse error (stdout: %s)", result.stdout[:200])
    raise HTTPException(status_code=502, detail="invalid core output")


class OrderBookResponse(BaseModel):
    order_book: dict
    latency_ms: int


class TickerResponse(BaseModel):
    ticker: dict
    latency_ms: int


class SignalResponse(BaseModel):
    signal: dict


@app.get("/health")
def health():
    return {"status": "ok", "service": "crypto-engine-api"}


def _is_gmo_symbol(sym: str) -> bool:
    s = (sym or "").strip().upper()
    return bool(s) and not s.endswith("USDT") and "_" not in s


def _fetch_gmo_public(path: str, timeout: int = 10) -> dict:
    """GMO Public APIを標準libのみで取得する（キー不要・coreのEXCHANGE_IDに非依存）。"""
    import json as _json
    import urllib.request as _req

    url = f"https://api.coin.z.com/public{path}"
    req = _req.Request(url, headers={"User-Agent": "cryptoEngine-api"})
    with _req.urlopen(req, timeout=timeout) as resp:
        body = resp.read().decode("utf-8")
    env = _json.loads(body)
    if env.get("status") != 0:
        raise RuntimeError(f"gmo public error: status={env.get('status')}")
    return env.get("data")


def _gmo_orderbook_direct(sym: str, limit: int) -> dict:
    """GMO板を直接取得し、OrderBookResponse形式で返す。"""
    import time as _time

    start = _time.time()
    data = _fetch_gmo_public(f"/v1/orderbooks?symbol={sym}")
    bids = [{"price": float(b["price"]), "volume": float(b["size"])} for b in data.get("bids", []) if float(b.get("price", 0) or 0) > 0]
    asks = [{"price": float(a["price"]), "volume": float(a["size"])} for a in data.get("asks", []) if float(a.get("price", 0) or 0) > 0]
    bids = bids[:limit]
    asks = asks[:limit]
    if not bids or not asks:
        raise RuntimeError(f"gmo empty book for {sym}")
    mid = (bids[0]["price"] + asks[0]["price"]) / 2.0
    return {
        "order_book": {
            "symbol": sym,
            "bids": bids,
            "asks": asks,
            "timestamp": int(_time.time() * 1000),
            "mid_price": mid,
            "spread": asks[0]["price"] - bids[0]["price"],
        },
        "latency_ms": int((_time.time() - start) * 1000),
    }


def _gmo_ticker_direct(sym: str) -> dict:
    """GMOティッカーを直接取得し、TickerResponse形式で返す。"""
    import time as _time

    start = _time.time()
    rows = _fetch_gmo_public(f"/v1/ticker?symbol={sym}")
    if not rows:
        raise RuntimeError(f"gmo empty ticker for {sym}")
    t = rows[0]
    last = float(t.get("last", 0) or 0)
    bid = float(t.get("bestBid", 0) or 0)
    ask = float(t.get("bestAsk", 0) or 0)
    vol = float(t.get("volume", 0) or 0)
    if last <= 0 and bid > 0 and ask > 0:
        last = (bid + ask) / 2.0
    return {
        "ticker": {"symbol": sym, "last": last, "bid": bid, "ask": ask, "volume": vol},
        "latency_ms": int((_time.time() - start) * 1000),
    }


@app.get("/api/v1/orderbook", response_model=OrderBookResponse)
async def fetch_orderbook(
    symbol: str = Query("BTCUSDT", description="Trading pair"),
    limit: int = Query(50, ge=1, le=200, description="Order book depth"),
    _: str = Depends(security.require_auth),
):
    """板情報を返す。Binance形式（BTCUSDT等）はWSキャッシュ、GMO現物（BTC等）は直接取得。

    GMO Public APIはキー不要のため、GMO銘柄は未設定でも取得できる。
    coreのEXCHANGE_IDに依存しない（VPSがbinance設定でもGMO板が見られる）。
    """
    sym = (symbol or "").strip().upper()
    if _is_gmo_symbol(sym):
        try:
            data = _gmo_orderbook_direct(sym, limit)
            return OrderBookResponse(**data)
        except Exception as e:
            logger.warning("gmo orderbook direct failed (%s); trying core", e)
        data = _call_core_grpc(grpc_client.fetch_orderbook, sym, limit)
        if data is None:
            try:
                data = _run_core("fetch-orderbook", sym, str(limit))
            except HTTPException:
                raise HTTPException(
                    status_code=503,
                    detail=f"gmo orderbook not available for {sym}",
                )
        ob = data.get("order_book", data)
        return OrderBookResponse(order_book=ob, latency_ms=int(data.get("latency_ms", 0) or 0))
    order_book = await binance.latest()
    if order_book is None:
        raise HTTPException(
            status_code=503,
            detail="binance orderbook not available yet; ws stream connecting",
        )
    return OrderBookResponse(order_book=order_book, latency_ms=0)


@app.get("/api/v1/ticker", response_model=TickerResponse)
def fetch_ticker(
    symbol: str = Query("BTCUSDT", description="Trading pair"),
    _: str = Depends(security.require_auth),
):
    sym = (symbol or "").strip().upper()
    if _is_gmo_symbol(sym):
        try:
            return TickerResponse(**_gmo_ticker_direct(sym))
        except Exception as e:
            logger.warning("gmo ticker direct failed (%s); trying core", e)
    data = _call_core_grpc(grpc_client.fetch_ticker, symbol)
    if data is None:
        data = _run_core("fetch-ticker", symbol)
    return TickerResponse(**data)


@app.get("/api/v1/detect-signal", response_model=SignalResponse)
async def get_latest_signal(_: str = Depends(security.require_auth)):
    """1 秒間隔のシグナルループが生成した最新シグナルを返す。"""
    async with _signal_lock:
        signal = _latest_signal
    if signal is None:
        raise HTTPException(
            status_code=503,
            detail="no signal generated yet; waiting for binance orderbook",
        )
    return SignalResponse(signal=signal)


@app.post("/api/v1/detect-signal", response_model=SignalResponse)
async def detect_signal(
    order_book: Optional[dict] = Body(None),
    _: str = Depends(security.require_auth),
):
    """Binance WS の最新 orderbook（body が無い場合）で core の DetectSignal を実行する。"""
    if order_book is None:
        order_book = await binance.latest()
        if order_book is None:
            raise HTTPException(
                status_code=503,
                detail="binance orderbook not available yet; ws stream connecting",
            )
    data = await asyncio.to_thread(_detect_from_core, order_book)
    if data is None:
        raise HTTPException(status_code=502, detail="core signal detection unavailable")
    await asyncio.to_thread(process_signal, data.get("signal", {}))
    return SignalResponse(**data)


@app.get("/api/v1/balance")
def get_balance_endpoint(_: str = Depends(security.require_auth)):
    return _balance_from_real()


@app.get("/api/v1/trades")
def get_trades_endpoint(
    symbol: Optional[str] = Query(None, description="Filter by trading pair"),
    side: Optional[str] = Query(None, description="Filter by side (buy/sell)"),
    limit: int = Query(50, ge=1, le=200),
    _: str = Depends(security.require_auth),
):
    payload = _trades_from_sources(symbol=symbol, side=side, limit=limit)
    return payload


@app.get("/api/v1/trade-pairs")
def get_trade_pairs_endpoint(
    limit: int = Query(100, ge=1, le=500),
    _: str = Depends(security.require_auth),
):
    """BUY→SELL でペアリング済みのトレードを直近から返す。"""
    return _db_or_demo(
        lambda session: None,
        repository.read_trade_pairs,
        demo_get_trade_pairs,
        limit=limit,
    )


@app.get("/api/v1/positions")
def get_positions_endpoint(_: str = Depends(security.require_auth)):
    return _read_positions_real()


@app.get("/api/v1/signals")
def get_signals_endpoint(
    symbol: Optional[str] = Query(None, description="Filter by trading pair"),
    limit: int = Query(50, ge=1, le=200),
    _: str = Depends(security.require_auth),
):
    payload = _db_or_demo(
        repository.seed_signals,
        repository.read_signals,
        get_signals,
        symbol=symbol,
        limit=limit,
    )
    signals = payload["signals"]
    if symbol:
        signals = [s for s in signals if s["symbol"] == symbol]
    return {"signals": signals[:limit]}


def _read_market_weather() -> dict:
    """traderの天気スナップショットを読む。無ければstoppedを返す。"""
    import json as _json

    candidates = [
        os.environ.get("MARKET_WEATHER_PATH", ""),
        "/app/backend/core/trade_logs/market_weather.json",
        "backend/core/trade_logs/market_weather.json",
        str(Path(__file__).resolve().parent.parent / "core" / "trade_logs" / "market_weather.json"),
    ]
    now_ms = int(time.time() * 1000)
    for path in candidates:
        if not path:
            continue
        try:
            with open(path, encoding="utf-8") as f:
                w = _json.load(f)
            ts = int(w.get("ts", 0) or 0)
            age_s = max(0, (now_ms - ts) // 1000) if ts else -1
            symbols = w.get("symbols", {}) or {}
            approaching = sorted(s for s, v in symbols.items() if (v or {}).get("approaching"))
            return {
                "status": "running" if 0 <= age_s <= 120 else "stale",
                "age_sec": age_s,
                "count": len(symbols),
                "approaching": approaching,
                "symbols": symbols,
            }
        except (OSError, ValueError):
            continue
    return {"status": "stopped", "age_sec": -1, "count": 0, "approaching": [], "symbols": {}}


@app.get("/api/v1/market-weather")
def get_market_weather_endpoint(_: str = Depends(security.require_auth)):
    """全銘柄の相場天気予報（v2評価の内訳）。監視稼働の可視化用。"""
    return _read_market_weather()


@app.get("/api/v1/system")
def get_system_endpoint(_: str = Depends(security.require_auth)):
    return _build_system()
