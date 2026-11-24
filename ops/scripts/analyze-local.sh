#!/usr/bin/env bash
# cryptoEngine ops — run the v2 improvement loop locally on pulled VPS data.
#
# 【廃止】VPS 完結ポリシー（ops/PRODUCT_DATA_POLICY.md）により bulk pull を
# 停止したため、通常は使用しない。検証は VPS 上の手順（同文書「VPS 検証手順」）
# で行うこと。このスクリプトは BULK_PULL=1 で持ち帰ったデータがある場合のみ動く。
#
#   ops/scripts/analyze-local.sh [YYYYMMDD]
#
# Flow (read-only against ops/logs/<date>):
#   1) verify MANIFEST/MISSING (skip sweep input when market CSV is missing)
#   2) load db/tables.sql into a LOCAL verify database (never touches prod)
#   3) core sweep-v2 on the pulled orderbook CSV (+ --cal-out for the best run)
#   4) core report-v2 -> ops/reports/improve_YYYYMMDD.md
#   5) version the calibration JSON into ops/reports/calib/
#
# Requires: go toolchain (or prebuilt ./core binary via CORE_BIN), psql client
# for step 2 (optional — skipped with warning when absent).
set -euo pipefail

OPS_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DATE="${1:-$(date +%Y%m%d)}"
SRC="$OPS_ROOT/logs/$DATE"
REPORTS="$OPS_ROOT/reports"
CALIB_DIR="$REPORTS/calib"
WORK="$OPS_ROOT/logs/$DATE/.work"

# Local verify DB (dedicated database; prod credentials are never used here).
VERIFY_DB_URL="${VERIFY_DB_URL:-postgres://crypto:demo_local_password@localhost:5432/crypto_verify}"

# core binary resolution: $CORE_BIN, else ./core (repo root layout), else `go run`.
resolve_core() {
  if [ -n "${CORE_BIN:-}" ] && [ -x "$CORE_BIN" ]; then
    echo "$CORE_BIN"
  elif [ -x "$OPS_ROOT/../backend/core/core" ]; then
    echo "$OPS_ROOT/../backend/core/core"
  else
    echo "go-run"
  fi
}
core() {
  local bin
  bin="$(resolve_core)"
  if [ "$bin" = "go-run" ]; then
    (cd "$OPS_ROOT/../backend/core" && go run . "$@")
  else
    "$bin" "$@"
  fi
}

echo "==> analyze-local for $DATE (src=$SRC)"
[ -d "$SRC" ] || { echo "no pulled data at $SRC — run collect.sh first." >&2; exit 1; }
mkdir -p "$WORK" "$CALIB_DIR"

echo "==> (0/4) checking MISSING markers"
if [ -f "$SRC/MISSING.txt" ]; then
  cat "$SRC/MISSING.txt"
fi
CSV="$(ls -t "$SRC"/market/orderbook_*.csv 2>/dev/null | head -1 || true)"
if [ -z "$CSV" ]; then
  echo "no market CSV — sweep input unavailable. stopping (nothing inferred)." >&2
  exit 2
fi
echo "market CSV: $CSV"

echo "==> (1/4) loading tables.sql into verify DB (optional)"
if command -v psql >/dev/null 2>&1 && [ -f "$SRC/db/tables.sql" ] && grep -qE "COPY |INSERT INTO " "$SRC/db/tables.sql"; then
  psql "$VERIFY_DB_URL" -v ON_ERROR_STOP=1 -f "$SRC/db/tables.sql" >/dev/null \
    && echo "tables.sql loaded" \
    || echo "WARNING: tables.sql load failed — continuing without DB (sweep is in-memory)" >&2
else
  echo "WARNING: psql or table rows unavailable — continuing without DB (sweep is in-memory)" >&2
fi

echo "==> (2/4) sweep-v2"
SWEEP_OUT="$WORK/sweep_v2.json"
CAL_OUT="$WORK/calibration_best.json"
core sweep-v2 "$CSV" --out="$SWEEP_OUT" --cal-out="$CAL_OUT" --sort=profit_factor

echo "==> (3/4) report-v2"
REPORT="$REPORTS/improve_$DATE.md"
core report-v2 "$SWEEP_OUT" --out="$REPORT"
echo "report: $REPORT"

echo "==> (4/4) versioning calibration"
if [ -f "$CAL_OUT" ]; then
  CSVHASH="$(sha256sum "$CSV" | cut -c1-12)"
  DEST="$CALIB_DIR/calib_${CSVHASH}_$DATE.json"
  cp "$CAL_OUT" "$DEST"
  echo "calibration: $DEST"
  echo "next: review $REPORT, then seed live/paper via V2_CALIB_JSON=$DEST"
else
  echo "WARNING: no calibration exported (no eligible best run?) — seeding unavailable" >&2
fi

echo "==> done"
