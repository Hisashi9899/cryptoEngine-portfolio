"""funding_store / grid_math の単体テスト（依存なし・外部API不要）。"""

import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", "backend", "api"))

from funding_store import detect_reversal
from grid_math import build_levels, evaluate


def test_reversal_sign_flip():
    out = detect_reversal([0.0001, -0.0001])
    assert out["reversed"] is True
    assert out["prev"] == 0.0001


def test_reversal_no_flip():
    out = detect_reversal([0.0001, 0.0002])
    assert out["reversed"] is False


def test_reversal_spike():
    out = detect_reversal([0.0001, 0.001], spike_threshold=0.0005)
    assert out["spiked"] is True


def test_reversal_single_is_false():
    out = detect_reversal([0.0001])
    assert out["reversed"] is False


def test_grid_levels_arithmetic():
    lv = build_levels(90, 100, 10)
    assert len(lv) == 11
    assert lv[0] == 90
    assert lv[-1] == 100


def test_grid_levels_invalid():
    assert build_levels(100, 90, 10) == []
    assert build_levels(90, 100, 0) == []


def test_grid_evaluate_ok():
    out = evaluate(90, 100, 10, current_price=95.5, amount_per_grid=0.01, fee_bps=5)
    assert out["ok"] is True
    assert out["net_per_round"] is not None
    assert out["buy_levels"] + out["sell_levels"] == 11


def test_grid_evaluate_outside_warns():
    out = evaluate(90, 100, 10, current_price=120)
    assert out["ok"] is True
    assert any("outside" in w for w in out["warnings"])
