"""V.Tエンジン連携 API（読み取り専用・発注なし）。

- GET /api/v1/vt/signals?symbol=BTC&bars=300&days=7&limit=200
  vt_signals_*/vt_shadow_* JSONL を新しい順で返す。
- GET /api/v1/vt/score?days=7&src=signals&bars=0
  core の vt-score を subprocess で実行し、集計 JSON を返す。

DB・core常駐に依存しない。trade_logs ディレクトリ直読＋単発CLI。
"""

import glob as _glob
import json
import logging
import os
import subprocess
import time
from datetime import datetime, timezone
from pathlib import Path

from fastapi import APIRouter, Depends, HTTPException, Query

try:
    import auto_trade as _at
    import security
except ImportError:
    from .. import auto_trade as _at
    from .. import security

logger = logging.getLogger("crypto-api.vt")
router = APIRouter()

_CORE_DIR = Path(__file__).resolve().parent.parent / "core"


def _logs_dir() -> Path:
    tp = os.environ.get("TRADE_LOG_PATH", "")
    if tp.strip():
        return Path(tp).parent
    return _CORE_DIR / "trade_logs"


def _parse_ts_ms(value) -> int:
    try:
        return int(value or 0)
    except (TypeError, ValueError):
        return 0


@router.get("/api/v1/vt/signals")
def vt_signals(
    symbol: str = Query("", description="銘柄（空なら全銘柄）"),
    bars: int = Query(0, description="足秒フィルタ（0=全足種）"),
    days: int = Query(7, ge=0, le=90, description="遡及日数（0=全期間）"),
    limit: int = Query(200, ge=1, le=1000),
    src: str = Query("signals", description="signals|shadow|all"),
    _: str = Depends(security.require_auth),
):
    """V.T記録行を新しい順で返す（発注なし）。"""
    s = (src or "signals").strip().lower()
    if s not in ("signals", "shadow", "all"):
        raise HTTPException(status_code=400, detail=f"unsupported src: {src}")
    pats = []
    if s in ("signals", "all"):
        pats.append("vt_signals_*.jsonl")
    if s in ("shadow", "all"):
        pats.append("vt_shadow_*.jsonl")
    cutoff = 0
    if days and days > 0:
        cutoff = int((time.time() - days * 86400) * 1000)
    sym = (symbol or "").strip().upper()
    rows = []
    d = _logs_dir()
    for pat in pats:
        for path in sorted(_glob.glob(str(d / pat))):
            try:
                with open(path, encoding="utf-8") as f:
                    for line in f:
                        line = line.strip()
                        if not line:
                            continue
                        try:
                            r = json.loads(line)
                        except ValueError:
                            continue
                        if not isinstance(r, dict):
                            continue
                        ts = _parse_ts_ms(r.get("ts"))
                        if cutoff and ts < cutoff:
                            continue
                        if sym and str(r.get("symbol", "") or "").upper() != sym:
                            continue
                        if bars and bars > 0 and int(r.get("bars_sec", 0) or 0) != int(bars):
                            continue
                        rows.append(r)
            except OSError as exc:
                logger.warning("vt signals read failed %s (%s)", path, exc)
    rows.sort(key=lambda r: _parse_ts_ms(r.get("ts")), reverse=True)
    return {"signals": rows[:limit] if limit > 0 else rows, "count": len(rows)}


def _run_vt_score(days: int, src: str, bars: int, timeout: int = 90) -> dict:
    binary = _at._resolve_core_binary()
    if not binary or not Path(binary).exists():
        raise HTTPException(status_code=503, detail="core binary not available")
    cmd = [binary, "vt-score", f"--dir={_logs_dir()}", f"--days={int(days)}", f"--src={src}"]
    if bars and bars > 0:
        cmd.append(f"--bars={int(bars)}")
    try:
        proc = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)
    except subprocess.TimeoutExpired:
        raise HTTPException(status_code=504, detail="vt-score timed out")
    if proc.returncode != 0:
        detail = (proc.stderr.strip() or proc.stdout.strip())[-500:]
        raise HTTPException(status_code=502, detail=f"vt-score failed: {detail}")
    text = (proc.stdout or "").strip()
    try:
        return json.loads(text)
    except ValueError:
        start, end = text.find("{"), text.rfind("}")
        if start >= 0 and end > start:
            try:
                return json.loads(text[start:end + 1])
            except ValueError:
                pass
    raise HTTPException(status_code=502, detail="vt-score output invalid")


@router.get("/api/v1/vt/score")
def vt_score(
    days: int = Query(7, ge=0, le=90),
    src: str = Query("signals", description="signals|shadow|all"),
    bars: int = Query(0, description="足秒フィルタ（0=全足種）"),
    _: str = Depends(security.require_auth),
):
    """vt-score 集計を返す（発注なし・読み取り専用）。"""
    s = (src or "signals").strip().lower()
    if s not in ("signals", "shadow", "all"):
        raise HTTPException(status_code=400, detail=f"unsupported src: {src}")
    return _run_vt_score(days, s, bars)


@router.get("/api/v1/vt/status")
def vt_status(_: str = Depends(security.require_auth)):
    """V.T記録の鮮度（最終更新・件数）を返す。ダッシュボードの死活表示用。"""
    d = _logs_dir()
    now_ms = int(time.time() * 1000)
    files = sorted(_glob.glob(str(d / "vt_signals_*.jsonl")))
    latest, total = 0, 0
    try:
        for path in files:
            try:
                st = os.stat(path)
                if int(st.st_mtime * 1000) > latest:
                    latest = int(st.st_mtime * 1000)
                total += max(0, st.st_size)
            except OSError:
                continue
    except OSError:
        pass
    age_s = max(0, (now_ms - latest) // 1000) if latest else -1
    return {
        "files": len(files),
        "total_bytes": total,
        "latest_age_sec": age_s,
        "status": "running" if 0 <= age_s <= 3600 else ("stale" if age_s > 3600 else "stopped"),
    }
