"""watchdog.py の単体テスト（依存なし・外部通信なし）。"""

import csv
import os
import sys
from datetime import datetime, timedelta, timezone

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", "ops", "deploy"))

import watchdog


def _utc(y, mo, d, h, mi):
    return datetime(y, mo, d, h, mi, tzinfo=timezone.utc)


def test_maint_window_inside():
    # 土曜 09:30 JST == 土曜 00:30 UTC。
    assert watchdog.in_maint_window(_utc(2026, 9, 26, 0, 30)) is True
    assert watchdog.in_maint_window(_utc(2026, 9, 26, 1, 59)) is True


def test_maint_window_outside():
    # 土曜 08:59 JST / 11:00 JST は対象外。
    assert watchdog.in_maint_window(_utc(2026, 9, 25, 23, 59)) is False
    assert watchdog.in_maint_window(_utc(2026, 9, 26, 2, 0)) is False
    # 金曜・日曜は対象外。
    assert watchdog.in_maint_window(_utc(2026, 9, 25, 0, 30)) is False
    assert watchdog.in_maint_window(_utc(2026, 9, 27, 0, 30)) is False


def test_host_label_nonempty():
    assert watchdog.host_label()


def _write_log(path, rows):
    with open(path, "w", newline="") as f:
        w = csv.DictWriter(f, fieldnames=["timestamp", "action", "symbol", "price",
                                          "amount", "pnl", "reason", "order_id", "status"])
        w.writeheader()
        w.writerows(rows)


def _row(ts, status):
    return {"timestamp": ts.isoformat(), "action": "SELL", "symbol": "TST",
            "price": "1", "amount": "1", "pnl": "", "reason": "x",
            "order_id": "", "status": status}


def test_count_recent_errors(tmp_path):
    now = datetime(2026, 9, 26, 12, 0, tzinfo=timezone.utc)
    rows = [_row(now - timedelta(minutes=m), "error") for m in (5, 10, 70)]
    rows += [_row(now - timedelta(minutes=5), "filled")]
    _write_log(str(tmp_path / "trade_log.csv"), rows)
    assert watchdog.count_recent_errors(str(tmp_path), now) == 2


def test_count_recent_errors_missing():
    now = datetime(2026, 9, 26, 12, 0, tzinfo=timezone.utc)
    assert watchdog.count_recent_errors("/nonexistent-dir-xyz", now) == 0
