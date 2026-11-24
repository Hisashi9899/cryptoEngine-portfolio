"""グリッド評価 API（読み取り・計算のみ・発注なし・DB不使用）。

- POST /api/v1/grid/evaluate

既存の取引ループ・core・DBとは完全分離した戦略プラグイン。
自動発注はしない。VPSの検証・取引容量に影響しない。
"""

import logging

from fastapi import APIRouter, Body, Depends
from pydantic import BaseModel

try:
    import grid_math as _gm
    import security
except ImportError:
    from .. import grid_math as _gm
    from .. import security

logger = logging.getLogger("crypto-api.grid")
router = APIRouter()


class GridRequest(BaseModel):
    lower: float
    upper: float
    grids: int = 10
    current_price: float = 0
    amount_per_grid: float = 0
    fee_bps: float = 5.0
    mode: str = "arithmetic"


@router.post("/api/v1/grid/evaluate")
def grid_evaluate(
    req: GridRequest = Body(...),
    _: str = Depends(security.require_auth),
):
    """グリッド配置と1往復概算を返す（発注なし）。"""
    try:
        n = int(req.grids)
    except (TypeError, ValueError):
        return {"ok": False, "levels": [], "message": "invalid grids"}
    if n < 1 or n > 500:
        return {"ok": False, "levels": [], "message": "grids must be 1..500"}
    out = _gm.evaluate(
        lower=req.lower,
        upper=req.upper,
        grids=n,
        current_price=req.current_price,
        amount_per_grid=req.amount_per_grid,
        fee_bps=req.fee_bps,
        mode=req.mode,
    )
    return out
