"""equity_math の単体テスト（依存なし・DB/GMO不要）。"""

import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", "backend", "api"))

from equity_math import compute_equity, pnl_vs_history, shape_history


def test_jpy_only():
    out = compute_equity(
        [{"symbol": "JPY", "balance": "4986", "available": "4986"}],
        {},
    )
    assert out["total_jpy"] == 4986.0
    assert out["jpy_available"] == 4986.0
    assert out["holdings"] == [
        {"symbol": "JPY", "balance": 4986.0, "price_jpy": 1.0, "value_jpy": 4986.0}
    ]


def test_crypto_marked_to_market():
    out = compute_equity(
        [
            {"symbol": "JPY", "balance": "4000", "available": "4000"},
            {"symbol": "BTC", "balance": "0.0001", "available": "0.0001"},
            {"symbol": "XRP", "balance": "0", "available": "0"},
        ],
        {"BTC": 12_000_000.0},
    )
    assert out["total_jpy"] == 5200.0
    assert out["holdings"][0]["symbol"] == "JPY"
    assert out["holdings"][1]["symbol"] == "BTC"
    assert out["holdings"][1]["value_jpy"] == 1200.0


def test_missing_price_is_zero_but_kept():
    out = compute_equity([{"symbol": "DOGE", "balance": "10", "available": "10"}], {})
    assert out["total_jpy"] == 0.0
    assert out["holdings"][0]["symbol"] == "DOGE"
    assert out["holdings"][0]["value_jpy"] == 0.0


def test_garbage_input_never_explodes():
    out = compute_equity(
        [{"symbol": "BTC", "balance": "NaN", "available": None}, {"nope": 1}, None],
        None,
    )
    assert out["total_jpy"] == 0.0


def test_shape_history_sorts_and_caps():
    rows = [(3000, 30.0), (1000, 10.0), (2000, 20.0)]
    pts = shape_history(rows, limit=2)
    assert [p["balance"] for p in pts] == [20.0, 30.0]


def test_pnl_vs_history_empty_is_none():
    assert pnl_vs_history(100.0, []) is None
