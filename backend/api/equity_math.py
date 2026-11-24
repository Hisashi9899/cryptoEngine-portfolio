"""実資産エクイティの純粋計算（依存なし・単体テスト対象）。

GMO口座残高（JPY＋現物クリプト）と板midから時価総額を算出する。
paper会計（初期資金100万円）は触らない。表示層が LIVE/PAPER を切り替える。
"""

SNAPSHOT_MIN_INTERVAL_SEC = 300
SNAPSHOT_RETENTION_DAYS = 90

import time
from datetime import datetime, timezone


def _f(value) -> float:
    try:
        v = float(value or 0)
    except (TypeError, ValueError):
        return 0.0
    # NaN/inf を弾く（JSON化・DB格納の安全弁）。
    if v != v or v in (float("inf"), float("-inf")):
        return 0.0
    return v


def compute_equity(assets: list, prices: dict) -> dict:
    """GMO残高＋銘柄別JPY価格から実資産を算出する。

    assets: [{"symbol": "BTC", "balance": "0.01", "available": "0.01"}, ...]
      balance が無ければ available を使う。JPY は残高をそのまま加算する。
    prices: {"BTC": 12000000.0, ...}（板mid・JPY建て）
    価格不明のクリプトは 0 円扱い（持分を見落とさないよう holdings に残す）。
    """
    holdings = []
    total = 0.0
    jpy_balance = 0.0
    jpy_available = 0.0
    for a in assets or []:
        sym = str((a or {}).get("symbol", "") or "").upper()
        if not sym:
            continue
        bal = _f((a or {}).get("balance", None))
        if bal <= 0:
            bal = _f((a or {}).get("available", None))
        if sym == "JPY":
            jpy_balance = bal
            jpy_available = _f((a or {}).get("available", None))
            total += bal
            holdings.append({
                "symbol": "JPY",
                "balance": bal,
                "price_jpy": 1.0,
                "value_jpy": round(bal, 2),
            })
            continue
        if bal <= 0:
            continue
        price = _f((prices or {}).get(sym, 0))
        value = round(bal * price, 2) if price > 0 else 0.0
        total += value
        holdings.append({
            "symbol": sym,
            "balance": bal,
            "price_jpy": price,
            "value_jpy": value,
        })
    holdings.sort(key=lambda h: h["value_jpy"], reverse=True)
    return {
        "total_jpy": round(total, 2),
        "jpy_balance": round(jpy_balance, 2),
        "jpy_available": round(jpy_available, 2),
        "holdings": holdings,
    }


def shape_history(rows: list, limit: int = 500) -> list:
    """DB行 [(ts_ms, total)] → チャート用 [{timestamp, balance}]（昇順・上限付き）。"""
    pts = [
        {"timestamp": _iso_ms(ts), "balance": round(_f(total), 2)}
        for ts, total in (rows or [])
        if _f(total) >= 0 and ts
    ]
    pts.sort(key=lambda p: p["timestamp"])
    return pts[-limit:] if limit > 0 else pts


def _iso_ms(ts_ms: int) -> str:
    try:
        return datetime.fromtimestamp(int(ts_ms) / 1000, tz=timezone.utc).isoformat()
    except (TypeError, ValueError, OSError, OverflowError):
        return ""


def pnl_vs_history(total: float, history: list, hours: int = 24) -> float | None:
    """history 内で hours 時間以上前の最新点との差を返す。無ければ最古点との差。
    比較点が無ければ None。"""
    if not history:
        return None
    cutoff_ms = int((time.time() - hours * 3600) * 1000)
    base = None
    for p in history:
        try:
            ts = int(datetime.fromisoformat(p["timestamp"]).timestamp() * 1000)
        except (TypeError, ValueError, KeyError):
            continue
        if ts <= cutoff_ms:
            base = p["balance"]
        else:
            break
    if base is None:
        base = history[0]["balance"]
    try:
        return round(float(total) - float(base), 2)
    except (TypeError, ValueError):
        return None
