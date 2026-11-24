"""funding_math の単体テスト（依存なし・外部API不要）。"""

import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", "backend", "api"))

from funding_math import binance_error, funding_apr, is_bybit_success, net_apr_estimate, shape_rates


def test_apr_basic():
    # 0.01%/8h -> 0.0001*3*365*100 = 10.95%
    assert funding_apr(0.0001) == 10.95


def test_apr_negative_kept():
    assert funding_apr(-0.0002) == -21.9


def test_apr_garbage_is_zero():
    assert funding_apr("NaN") == 0.0
    assert funding_apr(None) == 0.0
    assert funding_apr(0.0001, settlements_per_day=0) == 0.0


def test_net_apr():
    assert net_apr_estimate(10.95, 2.0) == 8.95


def test_shape_sorts_desc():
    rows = [
        {"exchange": "binance", "symbol": "BTCUSDT", "funding_rate": 0.0001},
        {"exchange": "binance", "symbol": "ETHUSDT", "funding_rate": 0.0005},
    ]
    out = shape_rates(rows)
    assert [r["symbol"] for r in out] == ["ETHUSDT", "BTCUSDT"]
    assert out[0]["apr_pct"] == funding_apr(0.0005)


def test_is_bybit_success_zero_is_true():
    # 回帰: `or -1` 式だと 0 が falsy で失敗扱いになる
    assert is_bybit_success({"retCode": 0}) is True
    assert is_bybit_success({"retCode": "0"}) is True
    assert is_bybit_success({"retCode": 10001}) is False
    assert is_bybit_success({}) is False
    assert is_bybit_success([]) is False
    assert is_bybit_success(None) is False


def test_binance_error_dict_detected():
    assert binance_error({"code": -1121, "msg": "Invalid symbol."}) == "Invalid symbol."
    assert binance_error([{"fundingRate": "0.1"}]) is None
    assert binance_error({}) is None
