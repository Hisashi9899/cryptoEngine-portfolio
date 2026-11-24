"""バックテスト API。

CSV アップロードを受け取り、core の backtest CLI（Go）を subprocess で実行して
BacktestResult を JSON で返す。リアルタイム処理（gRPC / Binance WS）とは完全に分離され、
DB には一切書き込まない。
"""

import json
import logging
import os
import subprocess
import tempfile
from pathlib import Path

from fastapi import APIRouter, Depends, File, HTTPException, UploadFile

try:
    import security
except ImportError:
    from .. import security

logger = logging.getLogger("crypto-api.backtest")
router = APIRouter()

# subprocess タイムアウト（秒）。CSV が大きい場合に備えて余裕を持たせる。
_SUBPROCESS_TIMEOUT_SEC = 120


def _resolve_core_binary() -> str | None:
    """core バックテストバイナリのパスを解決する。

    /app/backend は compose のボリュームで上書きされるため、画像内の
    /usr/local/bin/core を優先する。
    """
    candidates = [
        os.environ.get("CORE_BACKTEST_BIN"),
        "/usr/local/bin/core",
        str(Path(__file__).resolve().parent.parent.parent / "core" / "core"),
        os.environ.get("CORE_BIN_PATH"),
    ]
    for c in candidates:
        if c and Path(c).exists():
            return c
    return candidates[0]


def _extract_json_line(stdout: str) -> dict:
    """stdout から JSON 1 行を取り出す。

    core バイナリは起動時に [INFO] ログを stdout へ出し、JSON は printJSON で
    単一行で出力する。そのため、JSON として解釈できる行を探して返す。
    """
    for line in stdout.splitlines():
        line = line.strip()
        if line.startswith("{") and line.endswith("}"):
            try:
                return json.loads(line)
            except json.JSONDecodeError:
                continue
    raise ValueError("no JSON line found in backtest output")


@router.post("/api/v1/backtest")
async def run_backtest(
    file: UploadFile = File(...),
    _: str = Depends(security.require_auth),
):
    """CSV（timestamp, midPrice, spread, bids(json), asks(json)）を受け取り、
    バックテスト結果を返す。"""
    if not (file.filename or "").endswith(".csv"):
        raise HTTPException(status_code=400, detail="CSV file required (.csv)")

    binary = _resolve_core_binary()
    if not binary or not Path(binary).exists():
        logger.error("core backtest binary not found (resolved=%s)", binary)
        raise HTTPException(status_code=503, detail="core backtest binary not available")

    data = await file.read()
    if len(data) > 50 * 1024 * 1024:
        raise HTTPException(status_code=413, detail="CSV too large (max 50MB)")

    fd, tmp = tempfile.mkstemp(suffix=".csv", prefix="backtest_")
    try:
        with os.fdopen(fd, "wb") as f:
            f.write(data)

        proc = subprocess.run(
            [binary, "backtest", tmp],
            capture_output=True,
            text=True,
            timeout=_SUBPROCESS_TIMEOUT_SEC,
        )
        if proc.returncode != 0:
            detail = (proc.stderr or proc.stdout or "").strip()[-2000:]
            raise HTTPException(status_code=400, detail=f"backtest failed: {detail}")
        try:
            return _extract_json_line(proc.stdout)
        except ValueError as e:
            logger.error("backtest output parse failed: %s", e)
            raise HTTPException(status_code=500, detail=str(e))
    except subprocess.TimeoutExpired:
        raise HTTPException(status_code=504, detail="backtest timed out")
    finally:
        try:
            os.unlink(tmp)
        except OSError:
            pass
