"""自動売買の実行オーケストレーション。

シグナルループ（main.py の _signal_loop）から呼ばれ、AUTO_TRADE_ENABLED が
ON のときのみ core CLI（execute-order）を subprocess で実行して実際の注文を出す。

- ランタイムの ON/OFF は API がオーソリティ。初期値は env の AUTO_TRADE_ENABLED。
- リスク管理（最大ポジション / 日次損失 / STOP_ON_LOSS 自動停止）は core 側
  （usecase/trading/risk.go）で実施し、結果を JSON で受け取る。
- 日次損失リミットで core が停止した場合、API 側の ON/OFF も自動で OFF にする。
- 監査ログ（trade_log.csv）は core 側で書き込み、ここでは読み出して表示する。
"""

import csv
import json
import logging
import os
import subprocess
import threading
from datetime import datetime, timezone
from pathlib import Path

logger = logging.getLogger("crypto-api.auto_trade")


def _f(value: str | None) -> float | None:
    """CSV 文字列を float に変換する（空は None）。"""
    if value is None or value == "":
        return None
    try:
        return float(value)
    except (TypeError, ValueError):
        return None

_CORE_DIR = Path(__file__).resolve().parent.parent / "core"

TRADE_LOG_CSV = Path(
    os.environ.get("TRADE_LOG_PATH", str(_CORE_DIR / "trade_logs" / "trade_log.csv"))
)

# --- リスク管理の env（表示用。実際の判定は core 側） --------------------------
MAX_POSITION_SIZE_USDT = float(os.environ.get("MAX_POSITION_SIZE_USDT", "100"))
MAX_DAILY_LOSS_USDT = float(os.environ.get("MAX_DAILY_LOSS_USDT", "20"))
TRADE_SIZE_USDT = float(os.environ.get("TRADE_SIZE_USDT", "50"))
STOP_ON_LOSS = os.environ.get("STOP_ON_LOSS", "true").strip().lower() == "true"

# --- ランタイム状態（API がオーソリティ。env の AUTO_TRADE_ENABLED が初期値） --
_state_lock = threading.Lock()
_auto_trade_enabled = os.environ.get("AUTO_TRADE_ENABLED", "false").strip().lower() == "true"
_risk_stopped = False


def is_enabled() -> bool:
    with _state_lock:
        return _auto_trade_enabled


def set_enabled(enabled: bool) -> None:
    """自動売買の ON/OFF を設定する。ON にすると risk 停止状態も解除する。"""
    global _auto_trade_enabled, _risk_stopped
    with _state_lock:
        _auto_trade_enabled = bool(enabled)
        if _auto_trade_enabled:
            _risk_stopped = False
    logger.info("auto trade set to %s", "ON" if _auto_trade_enabled else "OFF")


def is_risk_stopped() -> bool:
    with _state_lock:
        return _risk_stopped


def _set_risk_stopped() -> None:
    global _risk_stopped
    with _state_lock:
        _risk_stopped = True


def _resolve_core_binary() -> str | None:
    candidates = [
        os.environ.get("CORE_BACKTEST_BIN"),
        "/usr/local/bin/core",
        str(_CORE_DIR / "core"),
        os.environ.get("CORE_BIN_PATH"),
    ]
    for c in candidates:
        if c and Path(c).exists():
            return c
    return candidates[0]


def _read_trade_log_rows(limit: int) -> list[dict]:
    """trade_log.csv を新しい順で読み込む（無ければ空リスト）。"""
    if not TRADE_LOG_CSV.exists():
        return []
    rows = []
    try:
        with TRADE_LOG_CSV.open("r", encoding="utf-8", newline="") as f:
            reader = csv.DictReader(f)
            for row in reader:
                rows.append(row)
    except OSError as exc:
        logger.warning("failed to read trade log (%s)", exc)
        return []
    rows.reverse()
    return rows[:limit] if limit > 0 else rows


def read_trade_log(limit: int = 100) -> dict:
    """自動売買ログ（trade_log.csv）を API レスポンス形式で返す。"""
    logs = []
    for row in _read_trade_log_rows(limit):
        logs.append(
            {
                "timestamp": row.get("timestamp", ""),
                "action": row.get("action", ""),
                "symbol": row.get("symbol", ""),
                "price": _f(row.get("price")),
                "amount": _f(row.get("amount")),
                "pnl": _f(row.get("pnl")) if row.get("pnl") else None,
                "reason": row.get("reason", ""),
                "order_id": row.get("order_id", ""),
                "status": row.get("status", ""),
            }
        )
    return {"logs": logs}


def daily_pnl_usdt() -> float:
    """今日（UTC）の確定損益（USDT）合計を trade log から計算する。"""
    today = datetime.now(timezone.utc).date().isoformat()
    total = 0.0
    for row in _read_trade_log_rows(2000):
        if row.get("action") != "SELL" or not row.get("pnl"):
            continue
        ts = row.get("timestamp", "")
        try:
            if datetime.fromisoformat(ts).astimezone(timezone.utc).date().isoformat() == today:
                total += float(row["pnl"])
        except (ValueError, TypeError):
            continue
    return round(total, 6)


def _run_execute(symbol: str, side: str, price: float, reason: str) -> dict | None:
    """core の execute-order を subprocess で実行し、結果 JSON を返す。"""
    binary = _resolve_core_binary()
    if not binary or not Path(binary).exists():
        logger.error("core binary not found (resolved=%s)", binary)
        return None

    cmd = [
        binary,
        "execute-order",
        symbol,
        side,
        f"--price={price}",
        f"--reason={reason}",
        f"--auto-trade={str(is_enabled()).lower()}",
    ]
    logger.info("Executing: %s", " ".join(cmd))
    try:
        proc = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
    except subprocess.TimeoutExpired:
        logger.error("execute-order timed out")
        return None
    if proc.returncode != 0:
        logger.error("execute-order failed (rc=%d): %s", proc.returncode, proc.stderr.strip())
        return None
    try:
        return json.loads(proc.stdout)
    except json.JSONDecodeError as exc:
        logger.error("execute-order output invalid: %s", exc)
        return None


def maybe_execute(signal: dict) -> dict | None:
    """1 つのシグナルを自動売買に反映する。ON かつ BUY/SELL のときのみ注文する。

    HOLD・OFF 時は None を返す。実行結果（core の ExecuteOrderOutput）を返す。
    """
    if not is_enabled():
        return None
    if is_risk_stopped():
        logger.warning("auto trade skipped: risk stop is active")
        return None

    signal_type = (signal.get("type") or "hold").lower()
    symbol = signal.get("symbol", "")
    if not symbol or signal_type not in ("buy", "sell"):
        return None

    side = "buy" if signal_type == "buy" else "sell"
    price = float(signal.get("mid_price") or 0)
    if price <= 0:
        logger.warning("auto trade skipped: invalid price for %s", symbol)
        return None

    result = _run_execute(symbol, side, price, "signal")
    if result is None:
        return None

    # core が日次損失リミットで自動停止した場合、API 側の ON/OFF も OFF に合わせる。
    risk = result.get("risk") or {}
    if risk.get("stopped") and STOP_ON_LOSS:
        set_enabled(False)
        _set_risk_stopped()
        logger.warning("auto trade auto-stopped by risk management: %s", result.get("message", ""))

    status = result.get("status", "error")
    logger.info(
        "auto trade result: status=%s symbol=%s side=%s order=%s pnl=%s message=%s",
        status,
        symbol,
        side,
        result.get("order", {}).get("order_id", "-"),
        result.get("pnl_usdt", 0),
        result.get("message", ""),
    )
    return result
