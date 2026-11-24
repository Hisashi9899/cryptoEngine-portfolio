#!/usr/bin/env bash
# Runs ON the VPS (via cron). Bounded retention for the VPS-contained data loop.
#
# Keeps raw market data short-window (recency beats volume for microstructure
# calibration) while never touching audit/liveness state.
#
# Env (all optional):
#   LOG_DIR            trade_logs dir (default: backend/core/trade_logs under repo)
#   OPS_LOG_DIR        ops/logs dir (default: ops/logs under repo)
#   CSV_DAYS=14        orderbook_*.csv retention (as .csv; older are gzipped first)
#   GZIP_DAYS=14       orderbook_*.csv older than this are gzip-compressed in place
#                      (readers handle .csv.gz transparently; --delete never sees them
#                      as long as rsync-excludes lists *.csv.gz)
#   GZKEEP_DAYS=90     orderbook_*.csv.gz retention (deep history for research)
#   JSONL_DAYS=90      signals_v2_*/vt_signals_*/vt_shadow_* retention
#                      （V.T の前向き検証は 26–41日必要。14日だと基準データが
#                        検証中に消えるため 90日に延長。件数は小さく容量影響は軽微）
#   SHADOW_LINES=20000 shadow_edge.jsonl cap (lines, tail-kept)
#   CALIB_DAYS=60      calibration_YYYYMMDD.json retention (calibration_latest.json kept)
#   CRONLOG_LINES=2000 per-file cap for ops/logs/cron-*.log
#   PRODUCT_DIRS       space-separated product subdirs (default: "spot funding grid")
#   PRODUCT_DAYS       retention inside product subdirs (default: 14)
#   DRY_RUN=1          print only, delete nothing
#
# NEVER touches: trade_log.csv (audit), trader_heartbeat.json (liveness),
# calibration_latest.json (live seed), *.tmp (in-flight writes).
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# ops/scripts/remote から repo 直下へは3階層上（remote→scripts→ops→repo）。
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
LOG_DIR="${LOG_DIR:-$REPO_ROOT/backend/core/trade_logs}"
OPS_LOG_DIR="${OPS_LOG_DIR:-$REPO_ROOT/ops/logs}"
CSV_DAYS="${CSV_DAYS:-14}"
GZIP_DAYS="${GZIP_DAYS:-14}"
GZKEEP_DAYS="${GZKEEP_DAYS:-90}"
JSONL_DAYS="${JSONL_DAYS:-90}"
SHADOW_LINES="${SHADOW_LINES:-20000}"
CALIB_DAYS="${CALIB_DAYS:-60}"
CRONLOG_LINES="${CRONLOG_LINES:-2000}"
PRODUCT_DIRS="${PRODUCT_DIRS:-spot funding grid}"
PRODUCT_DAYS="${PRODUCT_DAYS:-14}"
DRY_RUN="${DRY_RUN:-0}"

deleted_files=0
deleted_bytes=0

prune_old() {
  # prune_old <dir> <glob> <days> : delete files older than <days>.
  local dir="$1" glob="$2" days="$3"
  [ -d "$dir" ] || return 0
  while IFS= read -r -d '' f; do
    size=$(stat -c%s "$f" 2>/dev/null || echo 0)
    if [ "$DRY_RUN" = "1" ]; then
      echo "would delete: $f (${size}B)"
    else
      rm -f "$f"
      echo "deleted: $f (${size}B)"
    fi
    deleted_files=$((deleted_files + 1))
    deleted_bytes=$((deleted_bytes + size))
  done < <(find "$dir" -maxdepth 1 -type f -name "$glob" -mtime +"$days" -print0 2>/dev/null)
}

trim_tail() {
  # trim_tail <file> <lines> : keep last <lines> lines in place.
  local file="$1" lines="$2"
  [ -f "$file" ] || return 0
  local total
  total=$(wc -l < "$file" 2>/dev/null | tr -d '[:space:]') || return 0
  if [ -n "$total" ] && [ "$total" -gt "$lines" ]; then
    if [ "$DRY_RUN" = "1" ]; then
      echo "would trim: $file (${total} lines -> ${lines})"
    else
      tail -n "$lines" "$file" > "$file.tmp" && mv "$file.tmp" "$file"
      echo "trimmed: $file (${total} lines -> ${lines})"
    fi
  fi
}

echo "==> prune-trade-logs: LOG_DIR=$LOG_DIR DRY_RUN=$DRY_RUN"
echo "--- orderbook CSVs: gzip older than ${GZIP_DAYS}d, keep .gz ${GZKEEP_DAYS}d ---"
if [ -d "$LOG_DIR" ]; then
  while IFS= read -r -d '' f; do
    size=$(stat -c%s "$f" 2>/dev/null || echo 0)
    if [ "$DRY_RUN" = "1" ]; then
      echo "would gzip: $f (${size}B)"
    else
      if gzip -f "$f"; then
        echo "gzipped: $f (${size}B)"
      else
        echo "gzip FAILED: $f" >&2
      fi
    fi
  done < <(find "$LOG_DIR" -maxdepth 1 -type f -name "orderbook_*.csv" ! -name "*.csv.gz" -mtime +"$GZIP_DAYS" -print0 2>/dev/null)
fi
echo "--- orderbook CSVs older than ${CSV_DAYS}d ---"
prune_old "$LOG_DIR" "orderbook_*.csv" "$CSV_DAYS"
echo "--- orderbook CSV.gz older than ${GZKEEP_DAYS}d ---"
prune_old "$LOG_DIR" "orderbook_*.csv.gz" "$GZKEEP_DAYS"
echo "--- trade tape CSVs: gzip older than ${GZIP_DAYS}d, keep .gz ${GZKEEP_DAYS}d ---"
if [ -d "$LOG_DIR" ]; then
  while IFS= read -r -d '' f; do
    size=$(stat -c%s "$f" 2>/dev/null || echo 0)
    if [ "$DRY_RUN" = "1" ]; then
      echo "would gzip: $f (${size}B)"
    else
      if gzip -f "$f"; then
        echo "gzipped: $f (${size}B)"
      else
        echo "gzip FAILED: $f" >&2
      fi
    fi
  done < <(find "$LOG_DIR" -maxdepth 1 -type f -name "trade_tape_*.csv" ! -name "*.csv.gz" -mtime +"$GZIP_DAYS" -print0 2>/dev/null)
fi
prune_old "$LOG_DIR" "trade_tape_*.csv" "$CSV_DAYS"
prune_old "$LOG_DIR" "trade_tape_*.csv.gz" "$GZKEEP_DAYS"
echo "--- signals_v2 jsonl older than ${JSONL_DAYS}d ---"
prune_old "$LOG_DIR" "signals_v2_*.jsonl" "$JSONL_DAYS"
echo "--- vt_signals jsonl older than ${JSONL_DAYS}d ---"
prune_old "$LOG_DIR" "vt_signals_*.jsonl" "$JSONL_DAYS"
echo "--- vt_shadow jsonl older than ${JSONL_DAYS}d ---"
prune_old "$LOG_DIR" "vt_shadow_*.jsonl" "$JSONL_DAYS"
echo "--- dated calibrations older than ${CALIB_DAYS}d (latest kept) ---"
if [ -d "$LOG_DIR" ]; then
  while IFS= read -r -d '' f; do
    case "$f" in
      *calibration_latest.json) continue ;;
    esac
    size=$(stat -c%s "$f" 2>/dev/null || echo 0)
    if [ "$DRY_RUN" = "1" ]; then
      echo "would delete: $f (${size}B)"
    else
      rm -f "$f"
      echo "deleted: $f (${size}B)"
    fi
    deleted_files=$((deleted_files + 1))
    deleted_bytes=$((deleted_bytes + size))
  done < <(find "$LOG_DIR" -maxdepth 1 -type f -name "calibration_*.json" -mtime +"$CALIB_DAYS" -print0 2>/dev/null)
fi
echo "--- shadow_edge.jsonl cap ${SHADOW_LINES} lines ---"
trim_tail "$LOG_DIR/shadow_edge.jsonl" "$SHADOW_LINES"
echo "--- product subdirs (${PRODUCT_DIRS}, ${PRODUCT_DAYS}d) ---"
for sub in $PRODUCT_DIRS; do
  [ -d "$LOG_DIR/$sub" ] || continue
  prune_old "$LOG_DIR/$sub" "orderbook_*.csv" "$PRODUCT_DAYS"
  prune_old "$LOG_DIR/$sub" "*.jsonl" "$PRODUCT_DAYS"
done
echo "--- cron logs cap ${CRONLOG_LINES} lines each ---"
if [ -d "$OPS_LOG_DIR" ]; then
  for f in "$OPS_LOG_DIR"/cron-*.log; do
    [ -f "$f" ] || continue
    trim_tail "$f" "$CRONLOG_LINES"
  done
fi
echo "==> prune done: files=${deleted_files} bytes=${deleted_bytes} (untouched: trade_log.csv, trader_heartbeat.json, calibration_latest.json)"
du -sh "$LOG_DIR" 2>/dev/null || true
