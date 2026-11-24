"""グリッド戦略の純粋計算（依存なし・単体テスト対象・発注なし）。

既存の取引ループ・DB・coreとは完全分離。評価専用プラグイン。
"""

ARITHMETIC = "arithmetic"
GEOMETRIC = "geometric"


def _f(value) -> float:
    try:
        v = float(value or 0)
    except (TypeError, ValueError):
        return 0.0
    if v != v or v in (float("inf"), float("-inf")):
        return 0.0
    return v


def build_levels(lower: float, upper: float, grids: int, mode: str = ARITHMETIC) -> list:
    """グリッド境界レベルを grids+1 点返す。無効入力は空リスト。"""
    lo, hi = _f(lower), _f(upper)
    try:
        n = int(grids or 0)
    except (TypeError, ValueError):
        return []
    if lo <= 0 or hi <= lo or n < 1 or n > 500:
        return []
    mode = (mode or ARITHMETIC).strip().lower()
    if mode == GEOMETRIC:
        ratio = (hi / lo) ** (1.0 / n)
        return [round(lo * (ratio ** i), 8) for i in range(n + 1)]
    step = (hi - lo) / n
    return [round(lo + step * i, 8) for i in range(n + 1)]


def evaluate(
    lower: float,
    upper: float,
    grids: int,
    current_price: float,
    amount_per_grid: float = 0.0,
    fee_bps: float = 5.0,
    mode: str = ARITHMETIC,
) -> dict:
    """1往復の概算利益・警告を返す（概算・手数料控除後）。

    amount_per_grid: 1グリッドあたりの数量（原資産単位）
    fee_bps: 片道手数料bps（既定5bps=0.05%）
    """
    levels = build_levels(lower, upper, grids, mode)
    if not levels:
        return {"ok": False, "levels": [], "message": "invalid range (need 0 < lower < upper, 1 <= grids <= 500)"}
    try:
        n = int(grids)
    except (TypeError, ValueError):
        return {"ok": False, "levels": [], "message": "invalid grids"}
    cur = _f(current_price)
    amt = _f(amount_per_grid)
    fee = _f(fee_bps)
    mode_n = (mode or ARITHMETIC).strip().lower()
    if mode_n == GEOMETRIC:
        lo = levels[0]
        ratio = (levels[-1] / lo) ** (1.0 / n) if lo > 0 else 1.0
        step_rate = ratio - 1.0
        mid = cur if cur > 0 else (levels[0] + levels[-1]) / 2.0
        gross_per_round = mid * step_rate * amt if amt > 0 else mid * step_rate
    else:
        step = (levels[-1] - levels[0]) / n
        gross_per_round = step * amt if amt > 0 else step
    fee_per_round = 0.0
    if amt > 0 and cur > 0:
        notional = cur * amt * 2.0  # 往復
        fee_per_round = notional * (fee / 10000.0)
    elif cur > 0:
        fee_per_round = cur * 2.0 * (fee / 10000.0)
    net = round(gross_per_round - fee_per_round, 8)
    buys = sum(1 for lv in levels if cur <= 0 or lv < cur)
    sells = sum(1 for lv in levels if cur > 0 and lv > cur)
    warnings = []
    if cur > 0 and (cur < levels[0] or cur > levels[-1]):
        warnings.append("current price is outside the range (trend-break risk)")
    if net <= 0 and amt > 0:
        warnings.append("net per round <= 0 after fees (widen range or reduce grids)")
    if n > 100:
        warnings.append("too many grids: fee drag and thin-fill risk")
    return {
        "ok": True,
        "mode": mode_n,
        "levels": levels,
        "grids": n,
        "buy_levels": buys,
        "sell_levels": sells,
        "gross_per_round": round(gross_per_round, 8),
        "fee_per_round": round(fee_per_round, 8),
        "net_per_round": net,
        "warnings": warnings,
    }
