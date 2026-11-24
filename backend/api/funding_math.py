"""ファンディング監視の純粋計算（依存なし・単体テスト対象）。

- FRは8h毎決済が基本。APR換算は rate * 3 * 365。
- 表示層が LIVE/推定APR を切り替える。発注はしない。
"""

DEFAULT_SETTLEMENTS_PER_DAY = 3
DEFAULT_DAYS_PER_YEAR = 365


def _f(value) -> float:
    try:
        v = float(value or 0)
    except (TypeError, ValueError):
        return 0.0
    if v != v or v in (float("inf"), float("-inf")):
        return 0.0
    return v


def funding_apr(
    funding_rate: float,
    settlements_per_day: int = DEFAULT_SETTLEMENTS_PER_DAY,
    days_per_year: int = DEFAULT_DAYS_PER_YEAR,
) -> float:
    """1回分のFR → 推定APR(%)。マイナスは支払い側でそのまま負値。"""
    rate = _f(funding_rate)
    try:
        n = int(settlements_per_day or 0)
    except (TypeError, ValueError):
        return 0.0
    try:
        d = int(days_per_year or 0)
    except (TypeError, ValueError):
        return 0.0
    if n <= 0 or d <= 0:
        return 0.0
    return round(rate * n * d * 100.0, 4)


def net_apr_estimate(funding_apr_pct: float, cost_apr_pct: float = 0.0) -> float:
    """手数料・乖離コスト控除後の概算APR。概算のため丸めは2桁。"""
    return round(_f(funding_apr_pct) - _f(cost_apr_pct), 2)


def shape_rates(rows: list, limit: int = 100) -> list:
    """FR rows → APR付きでソート（APR降順・上限付き）。"""
    shaped = []
    for r in rows or []:
        if not isinstance(r, dict):
            continue
        sym = str((r or {}).get("symbol", "") or "").upper()
        if not sym:
            continue
        rate = _f((r or {}).get("funding_rate", 0))
        shaped.append(
            {
                "exchange": str((r or {}).get("exchange", "binance") or "binance"),
                "symbol": sym,
                "funding_rate": rate,
                "funding_time": (r or {}).get("funding_time", ""),
                "next_funding_time": (r or {}).get("next_funding_time", ""),
                "mark_price": _f((r or {}).get("mark_price", 0)) or None,
                "apr_pct": funding_apr(rate),
            }
        )
    shaped.sort(key=lambda x: x["apr_pct"], reverse=True)
    if limit and limit > 0:
        return shaped[:limit]
    return shaped


def is_bybit_success(payload: object) -> bool:
    """Bybit v5応答の成否判定（retCode==0）。

    注意: `payload.get("retCode", -1) or -1` と書くと 0 が falsy で -1 に
    化けるため、成功時も失敗扱いになる。直接比較すること。
    """
    if not isinstance(payload, dict):
        return False
    rc = payload.get("retCode", payload.get("retcode", -1))
    return rc == 0 or rc == "0"


def binance_error(payload: object) -> str | None:
    """Binanceエラー応答 {"code":..,"msg":..} ならmsgを返す。正常系はNone。

    Binanceは不正シンボル時にlistではなくdictを返す。放置すると
    FR=0の正常値と区別できなくなるため、呼び出し側で例外化すること。
    """
    if isinstance(payload, dict) and ("code" in payload or "msg" in payload):
        return str(payload.get("msg") or payload)[:200]
    return None
