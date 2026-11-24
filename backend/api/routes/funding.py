"""ファンディング監視 API（読み取り専用・発注なし）。

- GET /api/v1/funding/rates?symbols=BTCUSDT,ETHUSDT&exchange=binance|bybit|all
- GET /api/v1/funding/history?symbol=BTCUSDT&limit=20&exchange=binance|bybit
- GET /api/v1/funding/reversals?symbols=BTCUSDT,ETHUSDT&exchange=binance|bybit

Binance先物＋Bybit v5 linear対応。標準libのみ（キー不要）。
失敗時は502/503で理由を返す。DB・core不使用で既存検証に無干渉。
順次取得（バーストなし）でレート制限に配慮する。
"""

import json
import logging
import time
import urllib.parse as _parse
import urllib.request as _req

from fastapi import APIRouter, Depends, HTTPException, Query

try:
    import funding_math as _fm
    import funding_store as _store
    import security
except ImportError:
    from .. import funding_math as _fm
    from .. import funding_store as _store
    from .. import security

logger = logging.getLogger("crypto-api.funding")
router = APIRouter()

BINANCE_FAPI = "https://fapi.binance.com"
BYBIT_API = "https://api.bybit.com"
_DEFAULT_SYMBOLS = ["BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "DOGEUSDT"]


def _get_json(url: str, timeout: int = 10) -> object:
    req = _req.Request(url, headers={"User-Agent": "cryptoEngine-funding"})
    with _req.urlopen(req, timeout=timeout) as resp:
        return json.loads(resp.read().decode("utf-8"))


def _norm_symbol(sym: str) -> str:
    return (sym or "").strip().upper()


def _fetch_binance_one(sym: str) -> dict:
    """1銘柄の最新FR＋次回決済時刻＋mark価格を返す。"""
    s = _norm_symbol(sym)
    start = time.time()
    # 最新FR1件
    fr_url = f"{BINANCE_FAPI}/fapi/v1/fundingRate?symbol={_parse.quote(s)}&limit=1"
    fr_rows = _get_json(fr_url)
    err = _fm.binance_error(fr_rows)
    if err:
        raise RuntimeError(f"binance funding error for {s}: {err}")
    rate = 0.0
    ftime = ""
    if isinstance(fr_rows, list) and fr_rows:
        last = fr_rows[-1] or {}
        try:
            rate = float(last.get("fundingRate", 0) or 0)
        except (TypeError, ValueError):
            rate = 0.0
        try:
            ts = int(last.get("fundingTime", 0) or 0)
            if ts > 0:
                from datetime import datetime, timezone

                ftime = datetime.fromtimestamp(ts / 1000, tz=timezone.utc).isoformat()
        except (TypeError, ValueError):
            ftime = ""
    # 次回決済・mark価格
    next_time = ""
    mark = 0.0
    try:
        prem = _get_json(f"{BINANCE_FAPI}/fapi/v1/premiumIndex?symbol={_parse.quote(s)}")
        if isinstance(prem, dict):
            try:
                nts = int(prem.get("nextFundingTime", 0) or 0)
                if nts > 0:
                    from datetime import datetime, timezone

                    next_time = datetime.fromtimestamp(nts / 1000, tz=timezone.utc).isoformat()
            except (TypeError, ValueError):
                pass
            try:
                mark = float(prem.get("markPrice", 0) or 0)
            except (TypeError, ValueError):
                mark = 0.0
    except Exception as e:  # premiumIndex失敗は致命傷にしない
        logger.warning("premiumIndex failed for %s (%s)", s, e)
    latency = int((time.time() - start) * 1000)
    return {
        "exchange": "binance",
        "symbol": s,
        "funding_rate": rate,
        "funding_time": ftime,
        "next_funding_time": next_time,
        "mark_price": mark if mark > 0 else None,
        "latency_ms": latency,
    }


def _fetch_bybit_one(sym: str) -> dict:
    """Bybit v5 tickers(category=linear)から最新FR＋次回決済＋markを返す。"""
    import time as _time

    s = _norm_symbol(sym)
    start = _time.time()
    url = f"{BYBIT_API}/v5/market/tickers?category=linear&symbol={_parse.quote(s)}"
    data = _get_json(url)
    if not _fm.is_bybit_success(data):
        msg = str((data or {}).get("retMsg", "bybit error") if isinstance(data, dict) else data)[:200]
        raise RuntimeError(f"bybit tickers error for {s}: {msg}")
    lst = ((data.get("result") or {}).get("list")) or []
    if not lst:
        raise RuntimeError(f"bybit empty tickers for {s}")
    t = lst[0] or {}
    try:
        rate = float(t.get("fundingRate", 0) or 0)
    except (TypeError, ValueError):
        rate = 0.0
    try:
        mark = float(t.get("markPrice", 0) or 0)
    except (TypeError, ValueError):
        mark = 0.0
    next_time = ""
    try:
        nts = int(str(t.get("nextFundingTime", "") or "0").strip() or 0)
        if nts > 0:
            from datetime import datetime, timezone

            next_time = datetime.fromtimestamp(nts / 1000, tz=timezone.utc).isoformat()
    except (TypeError, ValueError, OSError):
        next_time = ""
    return {
        "exchange": "bybit",
        "symbol": s,
        "funding_rate": rate,
        "funding_time": "",
        "next_funding_time": next_time,
        "mark_price": mark if mark > 0 else None,
        "latency_ms": int((_time.time() - start) * 1000),
    }


def _fetch_one(exchange: str, sym: str) -> dict:
    if (exchange or "").strip().lower() == "bybit":
        out = _fetch_bybit_one(sym)
    else:
        out = _fetch_binance_one(sym)
    time.sleep(0.2)  # 順次取得の間隔（レート制限配慮）
    return out


@router.get("/api/v1/funding/rates")
def funding_rates(
    symbols: str = Query("", description="カンマ区切り (空なら既定5銘柄)"),
    exchange: str = Query("binance", description="binance|bybit|all"),
    _: str = Depends(security.require_auth),
):
    """複数銘柄の最新FR＋推定APRをAPR降順で返す（発注なし）。"""
    ex = (exchange or "binance").strip().lower()
    if ex not in ("binance", "bybit", "all"):
        raise HTTPException(status_code=400, detail=f"unsupported exchange: {exchange}")
    syms = [_norm_symbol(s) for s in (symbols or "").split(",") if s.strip()]
    if not syms:
        syms = list(_DEFAULT_SYMBOLS)
    # allは2所分の順次取得になるため10銘柄に制限（応答タイムアウト防止）
    syms = syms[:10] if ex == "all" else syms[:20]
    targets = ["binance", "bybit"] if ex == "all" else [ex]
    rows = []
    errors = []
    for t in targets:
        for s in syms:
            try:
                rows.append(_fetch_one(t, s))
            except Exception as e:
                logger.warning("funding fetch failed %s:%s (%s)", t, s, e)
                errors.append({"symbol": f"{t}:{s}", "message": str(e)[:200]})
    shaped = _fm.shape_rates(rows, limit=40)
    return {"rates": shaped, "errors": errors, "exchange": ex}


@router.get("/api/v1/funding/history")
def funding_history(
    symbol: str = Query("BTCUSDT"),
    limit: int = Query(20, ge=1, le=100),
    exchange: str = Query("binance"),
    _: str = Depends(security.require_auth),
):
    """FR履歴（直近N件＋APR付き）。チャート・反転検出用。"""
    ex = (exchange or "binance").strip().lower()
    s = _norm_symbol(symbol)
    if not s or ex not in ("binance", "bybit"):
        raise HTTPException(status_code=400, detail="binance|bybit symbol required")
    if ex == "bybit":
        return _bybit_history(s, int(limit))
    try:
        pts = _binance_history_points(s, int(limit))
    except RuntimeError as e:
        logger.warning("funding history failed %s (%s)", s, e)
        raise HTTPException(status_code=502, detail=f"binance funding history unavailable: {e}")
    return {"exchange": "binance", "symbol": s, "history": pts}


def _binance_history_points(symbol: str, limit: int) -> list:
    """Binance FR履歴を昇順ptsで返す。失敗時はRuntimeError（内部共用）。"""
    from datetime import datetime, timezone

    url = f"{BINANCE_FAPI}/fapi/v1/fundingRate?symbol={_parse.quote(symbol)}&limit={int(limit)}"
    data = _get_json(url)
    err = _fm.binance_error(data)
    if err:
        raise RuntimeError(f"binance funding history error for {symbol}: {err}")
    pts = []
    if isinstance(data, list):
        for row in data:
            try:
                rate = float((row or {}).get("fundingRate", 0) or 0)
            except (TypeError, ValueError):
                continue
            try:
                ts = int((row or {}).get("fundingTime", 0) or 0)
                iso = datetime.fromtimestamp(ts / 1000, tz=timezone.utc).isoformat() if ts else ""
            except (TypeError, ValueError, OSError):
                iso = ""
            pts.append({"timestamp": iso, "funding_rate": rate, "apr_pct": _fm.funding_apr(rate)})
    pts.sort(key=lambda p: p["timestamp"])
    return pts


def _bybit_history(symbol: str, limit: int) -> dict:
    """Bybit v5 funding履歴（新しい順で返るため昇順に直す）。"""
    try:
        url = (
            f"{BYBIT_API}/v5/market/funding/history?category=linear"
            f"&symbol={_parse.quote(symbol)}&limit={int(limit)}"
        )
        data = _get_json(url)
    except Exception as e:
        logger.warning("bybit funding history failed %s (%s)", symbol, e)
        raise HTTPException(status_code=502, detail=f"bybit funding history unavailable: {e}")
    if not isinstance(data, dict) or not _fm.is_bybit_success(data):
        raise HTTPException(status_code=502, detail=f"bybit funding history error: {str(data)[:200]}")
    pts = []
    for row in (((data.get("result") or {}).get("list")) or []):
        try:
            rate = float((row or {}).get("fundingRate", 0) or 0)
        except (TypeError, ValueError):
            continue
        try:
            ts = int(str((row or {}).get("fundingRateTimestamp", "") or "0").strip() or 0)
            if ts > 0:
                from datetime import datetime, timezone

                iso = datetime.fromtimestamp(ts / 1000, tz=timezone.utc).isoformat()
            else:
                iso = ""
        except (TypeError, ValueError, OSError):
            iso = ""
        pts.append({"timestamp": iso, "funding_rate": rate, "apr_pct": _fm.funding_apr(rate)})
    pts.sort(key=lambda p: p["timestamp"])
    return {"exchange": "bybit", "symbol": symbol, "history": pts[-limit:] if limit > 0 else pts}


@router.get("/api/v1/funding/reversals")
def funding_reversals(
    symbols: str = Query("", description="カンマ区切り (空なら既定5銘柄)"),
    exchange: str = Query("binance", description="binance|bybit"),
    lookback: int = Query(5, ge=2, le=50, description="直近N件で反転判定"),
    _: str = Depends(security.require_auth),
):
    """直近履歴から符号反転・急変を検出する（発注なし・VPS蓄積の代替にもなる）。"""
    ex = (exchange or "binance").strip().lower()
    if ex not in ("binance", "bybit"):
        raise HTTPException(status_code=400, detail=f"unsupported exchange: {exchange}")
    syms = [_norm_symbol(s) for s in (symbols or "").split(",") if s.strip()]
    if not syms:
        syms = list(_DEFAULT_SYMBOLS)
    syms = syms[:10]
    out = []
    for s in syms:
        try:
            # 直接取得（認証デコレータを避けるため内部関数を呼ぶ）
            if ex == "bybit":
                h = _bybit_history(s, int(lookback)).get("history", [])
            else:
                h = _binance_history_points(s, int(lookback))
            det = _store.detect_reversal(h)
            out.append({"exchange": ex, "symbol": s, **det, "points": len(h)})
        except HTTPException as e:
            out.append({"exchange": ex, "symbol": s, "error": e.detail})
        except Exception as e:
            logger.warning("reversal check failed %s:%s (%s)", ex, s, e)
            out.append({"exchange": ex, "symbol": s, "error": str(e)[:200]})
        time.sleep(0.2)
    return {"reversals": out, "exchange": ex, "lookback": int(lookback)}
