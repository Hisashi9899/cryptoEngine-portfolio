"""ローカル検証（短期キャプチャ → グリッド探索 → 統計レポート）API。

Binance WS を 1 秒間隔で 10〜60 分キャプチャし、その CSV でグリッド探索
バックテスト（sweep）→ 統計レポート生成（report）を core CLI（Go）の
subprocess として実行する。リアルタイム処理（gRPC / binance_ws.py）とは完全に
分離され、DB には一切書き込まない。成果物は backend/core 直下の tmp_* ファイル。
"""

import asyncio
import json
import logging
import os
import subprocess
import time
from pathlib import Path

from fastapi import APIRouter, Body, Depends, HTTPException
from pydantic import BaseModel

try:
    import security
except ImportError:
    from .. import security

logger = logging.getLogger("crypto-api.local_verify")
router = APIRouter()

_CORE_DIR = Path(__file__).resolve().parent.parent.parent / "core"
CAPTURE_CSV = _CORE_DIR / "tmp_local_capture.csv"
SWEEP_JSON = _CORE_DIR / "tmp_local_backtest_results.json"
REPORT_MD = _CORE_DIR / "tmp_local_report.md"

_SUBPROCESS_TIMEOUT_SEC = 120


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


def _count_rows(path: Path) -> int:
    if not path.exists():
        return 0
    try:
        with path.open("r", encoding="utf-8") as f:
            rows = sum(1 for _ in f)
        return max(0, rows - 1)  # ヘッダ行を除く
    except OSError:
        return 0


class CaptureState:
    status: str = "idle"  # idle | capturing | done | error
    started_at: int | None = None
    seconds: int | None = None
    rows: int | None = None
    error: str | None = None
    proc: asyncio.subprocess.Process | None = None
    task: asyncio.Task | None = None


_capture = CaptureState()


class CaptureStartRequest(BaseModel):
    minutes: float = 30


async def _await_capture() -> None:
    """capture subprocess の完了を待ち、ステータスを更新する。"""
    try:
        stdout, stderr = await _capture.proc.communicate()
        if _capture.proc.returncode != 0:
            _capture.status = "error"
            _capture.error = (stderr or stdout).decode(errors="replace").strip()[-2000:]
            logger.error("capture failed: %s", _capture.error)
            return
        # core の stdout 末尾に JSON 行（rows を含む）が出る。
        info = {}
        try:
            for line in stdout.decode(errors="replace").splitlines():
                line = line.strip()
                if line.startswith("{") and line.endswith("}"):
                    info = json.loads(line)
        except json.JSONDecodeError:
            pass
        _capture.rows = info.get("rows") if isinstance(info.get("rows"), int) else _count_rows(CAPTURE_CSV)
        _capture.status = "done"
        logger.info("local capture done: rows=%s", _capture.rows)
    except Exception as e:  # noqa: BLE001
        _capture.status = "error"
        _capture.error = str(e)
        logger.error("capture await failed: %s", e)
    finally:
        _capture.proc = None


async def shutdown() -> None:
    """アプリ停止時に実行中の capture を終了する。"""
    if _capture.status == "capturing" and _capture.proc is not None:
        _capture.proc.terminate()
        try:
            await asyncio.wait_for(_capture.proc.wait(), timeout=5)
        except asyncio.TimeoutError:
            _capture.proc.kill()
        _capture.status = "error"
        _capture.error = "capture terminated on shutdown"


@router.post("/api/v1/local-verify/capture")
async def start_capture(
    req: CaptureStartRequest = Body(default=CaptureStartRequest()),
    _: str = Depends(security.require_auth),
):
    """短期オーダーブックキャプチャを開始する（10〜60 分）。"""
    if _capture.status == "capturing":
        raise HTTPException(status_code=400, detail="capture already running")

    binary = _resolve_core_binary()
    if not binary or not Path(binary).exists():
        logger.error("core binary not found (resolved=%s)", binary)
        raise HTTPException(status_code=503, detail="core binary not available")

    minutes = min(max(req.minutes, 10), 60)
    seconds = int(minutes * 60)

    CAPTURE_CSV.parent.mkdir(parents=True, exist_ok=True)
    proc = await asyncio.create_subprocess_exec(
        binary,
        "capture-local",
        str(CAPTURE_CSV),
        f"--seconds={seconds}",
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE,
    )
    _capture.status = "capturing"
    _capture.started_at = int(time.time() * 1000)
    _capture.seconds = seconds
    _capture.rows = None
    _capture.error = None
    _capture.proc = proc
    _capture.task = asyncio.create_task(_await_capture())
    logger.info("started local capture: %d seconds -> %s", seconds, CAPTURE_CSV)
    return {
        "status": "capturing",
        "minutes": minutes,
        "seconds": seconds,
        "out_file": str(CAPTURE_CSV),
    }


@router.get("/api/v1/local-verify/capture")
async def capture_status(_: str = Depends(security.require_auth)):
    """キャプチャの状態（idle / capturing / done / error）と経過を返す。"""
    elapsed = None
    if _capture.started_at:
        elapsed = int((time.time() * 1000 - _capture.started_at) / 1000)
    rows = _capture.rows
    if _capture.status == "done" and rows is None:
        rows = _count_rows(CAPTURE_CSV)
    return {
        "status": _capture.status,
        "minutes": round((_capture.seconds or 0) / 60, 1),
        "elapsed_seconds": elapsed,
        "target_seconds": _capture.seconds,
        "rows": rows,
        "error": _capture.error,
        "out_file": str(CAPTURE_CSV),
    }


@router.post("/api/v1/local-verify/sweep")
async def run_sweep(_: str = Depends(security.require_auth)):
    """キャプチャ CSV に対してグリッド探索バックテストを実行し、結果を返す。"""
    if _capture.status == "capturing":
        raise HTTPException(status_code=400, detail="capture still running")
    if not CAPTURE_CSV.exists():
        raise HTTPException(status_code=404, detail="capture not found")

    binary = _resolve_core_binary()
    if not binary or not Path(binary).exists():
        raise HTTPException(status_code=503, detail="core binary not available")

    def _exec() -> subprocess.CompletedProcess:
        return subprocess.run(
            [binary, "sweep", str(CAPTURE_CSV), f"--out={SWEEP_JSON}"],
            capture_output=True,
            text=True,
            timeout=_SUBPROCESS_TIMEOUT_SEC,
        )

    try:
        proc = await asyncio.to_thread(_exec)
    except subprocess.TimeoutExpired:
        raise HTTPException(status_code=504, detail="sweep timed out")
    if proc.returncode != 0:
        detail = (proc.stderr or proc.stdout or "").strip()[-2000:]
        raise HTTPException(status_code=400, detail=f"sweep failed: {detail}")

    try:
        return json.loads(SWEEP_JSON.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as e:
        raise HTTPException(status_code=500, detail=f"sweep results unreadable: {e}")


@router.get("/api/v1/local-verify/sweep")
async def sweep_results(_: str = Depends(security.require_auth)):
    """最後に実行した探索結果 JSON を返す。"""
    if not SWEEP_JSON.exists():
        raise HTTPException(status_code=404, detail="sweep results not found")
    try:
        return json.loads(SWEEP_JSON.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as e:
        raise HTTPException(status_code=500, detail=f"sweep results unreadable: {e}")


@router.post("/api/v1/local-verify/report")
async def generate_report(_: str = Depends(security.require_auth)):
    """探索結果から統計レポート（Markdown）を生成する。"""
    if not SWEEP_JSON.exists():
        raise HTTPException(status_code=404, detail="sweep results not found")

    binary = _resolve_core_binary()
    if not binary or not Path(binary).exists():
        raise HTTPException(status_code=503, detail="core binary not available")

    def _exec() -> subprocess.CompletedProcess:
        return subprocess.run(
            [binary, "report", str(SWEEP_JSON), f"--out={REPORT_MD}"],
            capture_output=True,
            text=True,
            timeout=_SUBPROCESS_TIMEOUT_SEC,
        )

    try:
        proc = await asyncio.to_thread(_exec)
    except subprocess.TimeoutExpired:
        raise HTTPException(status_code=504, detail="report timed out")
    if proc.returncode != 0:
        detail = (proc.stderr or proc.stdout or "").strip()[-2000:]
        raise HTTPException(status_code=400, detail=f"report failed: {detail}")
    return {"status": "ok", "out_file": str(REPORT_MD)}


@router.get("/api/v1/local-verify/report")
async def report_content(_: str = Depends(security.require_auth)):
    """生成済みレポートの Markdown 内容を返す。"""
    if not REPORT_MD.exists():
        raise HTTPException(status_code=404, detail="report not generated yet")
    try:
        return {"content": REPORT_MD.read_text(encoding="utf-8")}
    except OSError as e:
        raise HTTPException(status_code=500, detail=f"report unreadable: {e}")
