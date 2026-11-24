"""FRログ蓄積（ファイル分離・既存DB/検証に無干渉）。

- 保存先: backend/core/trade_logs/funding.jsonl（環境変数 FUNDING_LOG_PATH で上書き）
- 形式: 1行1スナップショット {"ts": ms, "exchange": "binance", "rates": [...]}
- 保持: 90日・10万行上限でローテーション（equityと同方針）
- 反転検出は純粋関数でテスト可能にする。
"""

import json
import os
import time
from pathlib import Path

RETENTION_DAYS = 90
MAX_LINES = 100_000

_SELF_DIR = Path(__file__).resolve().parent
_DEFAULT_LOG = _SELF_DIR.parent / "core" / "trade_logs" / "funding.jsonl"


def log_path() -> Path:
    p = os.environ.get("FUNDING_LOG_PATH", str(_DEFAULT_LOG))
    return Path(p)


def _f(value) -> float:
    try:
        v = float(value or 0)
    except (TypeError, ValueError):
        return 0.0
    if v != v or v in (float("inf"), float("-inf")):
        return 0.0
    return v


def append_snapshot(exchange: str, rates: list, ts_ms: int = 0) -> str:
    """1スナップショットを追記し、保持条件で刈り込む。保存先パスを返す。"""
    path = log_path()
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
    except OSError:
        pass
    ts = int(ts_ms or time.time() * 1000)
    line = json.dumps(
        {"ts": ts, "exchange": exchange or "binance", "rates": rates or []},
        ensure_ascii=False,
    )
    try:
        with path.open("a", encoding="utf-8") as f:
            f.write(line + "\n")
    except OSError:
        return str(path)
    _prune(path)
    return str(path)


def _prune(path: Path) -> None:
    try:
        cutoff = int((time.time() - RETENTION_DAYS * 86400) * 1000)
    except (TypeError, ValueError):
        return
    try:
        with path.open("r", encoding="utf-8") as f:
            lines = f.readlines()
    except OSError:
        return
    if len(lines) <= MAX_LINES:
        # 行数内でも古い行だけ落とす
        kept = [ln for ln in lines if _line_ts(ln) >= cutoff]
        if len(kept) == len(lines):
            return
    else:
        kept = lines[-MAX_LINES:]
        kept = [ln for ln in kept if _line_ts(ln) >= cutoff]
    try:
        with path.open("w", encoding="utf-8") as f:
            f.writelines(kept)
    except OSError:
        pass


def _line_ts(line: str) -> int:
    try:
        return int((json.loads(line) or {}).get("ts", 0) or 0)
    except (TypeError, ValueError):
        return 0


def read_snapshots(limit: int = 200) -> list:
    """新しい順で最大limit件返す。無ければ空リスト。"""
    path = log_path()
    try:
        with path.open("r", encoding="utf-8") as f:
            lines = f.readlines()
    except OSError:
        return []
    out = []
    for ln in lines[-limit:] if limit > 0 else lines:
        try:
            out.append(json.loads(ln))
        except (TypeError, ValueError):
            continue
    return out


def detect_reversal(history: list, spike_threshold: float = 0.0005) -> dict:
    """FR履歴（古→新、要素はrate数値or辞書）から反転・急変を検出する。

    戻り値: {"reversed": bool, "spiked": bool, "prev": float, "last": float}
    """
    vals = []
    for h in history or []:
        if isinstance(h, dict):
            vals.append(_f(h.get("funding_rate", h.get("rate", 0))))
        else:
            vals.append(_f(h))
    if len(vals) < 2:
        return {"reversed": False, "spiked": False, "prev": vals[-1] if vals else 0.0, "last": vals[-1] if vals else 0.0}
    prev, last = vals[-2], vals[-1]
    reversed_ = (prev == 0 and last != 0) or (prev * last < 0)
    spiked = abs(last - prev) >= _f(spike_threshold)
    return {"reversed": bool(reversed_), "spiked": bool(spiked), "prev": prev, "last": last}
