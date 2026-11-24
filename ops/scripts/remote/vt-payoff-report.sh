#!/usr/bin/env bash
# 既存の反実仮想 run に対するパターン別ペイオフ分析（読み取り専用）。
#
#   bash ops/scripts/remote/vt-payoff-report.sh [run_dir] [--k "0 1.0"]
#
# run_dir 省略時は <trade_logs>/cf 配下の最新 run。5分足 CF の結果を
# そのまま分析でき、1時間足 CF（vt-timeframe-cf.sh）と同じ TSV 形式で
# ops/logs/vt-payoff.tsv に累積する（同じ run_id は二重追記しない）。
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
LOG_DIR="${VT_PAYOFF_LOG_DIR:-$REPO_ROOT/backend/core/trade_logs}"
OUT_TSV="${VT_PAYOFF_TSV:-$REPO_ROOT/ops/logs/vt-payoff.tsv}"
FEE="${VT_PAYOFF_FEE:-0.0005}"
KS="${VT_PAYOFF_KS:-0 1.0 2.5}"

RUN=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --k)
      shift
      _ks=""
      while [ "$#" -gt 0 ] && [ "${1#-}" = "$1" ]; do
        _ks="$_ks $1"; shift
      done
      [ -n "${_ks# }" ] && KS="${_ks# }"
      ;;
    --k=*) KS="${1#--k=}"; shift ;;
    -h|--help)
      echo "usage: $0 [run_dir] [--k \"0 1.0\"]"; exit 0 ;;
    -*) echo "unknown option: $1" >&2; exit 2 ;;
    *) RUN="$1"; shift ;;
  esac
done
if [ -z "$RUN" ]; then
  RUN="$(ls -1dt "$LOG_DIR"/cf/*/ 2>/dev/null | head -1)"
  RUN="${RUN%/}"
fi
if [ -z "$RUN" ] || [ ! -d "$RUN" ]; then
  echo "ERROR: run dir not found (usage: $0 [run_dir])" >&2
  exit 1
fi

RUN_ID="$(basename "$RUN")"
TMP="$RUN/payoff.tsv"
mkdir -p "$(dirname "$OUT_TSV")"

if ! python3 "$REPO_ROOT/ops/scripts/remote/vt-payoff-analysis.py" \
    --run-dir "$RUN" --k $KS --fee-rate="$FEE" --out "$TMP"; then
  echo "ERROR: payoff analysis failed" >&2
  exit 1
fi

if [ ! -s "$TMP" ]; then
  echo "no trades in $RUN (k=$KS)" >&2
  exit 0
fi

if [ -s "$OUT_TSV" ] && grep -q "^${RUN_ID}	" "$OUT_TSV"; then
  echo "already recorded: $RUN_ID (skip append)"
else
  if [ ! -s "$OUT_TSV" ]; then
    cat "$TMP" >>"$OUT_TSV"
  else
    tail -n +2 "$TMP" >>"$OUT_TSV"
  fi
  echo "appended: $OUT_TSV"
fi

echo
echo "=== pattern payoff $RUN_ID (fee=$FEE/side, k=$KS) ==="
awk -F'\t' 'NR==1 || $1=="'"$RUN_ID"'" {
  printf "%-5s %-4s %-5s %-22s %5s %7s %7s %7s %10s %10s\n",
    $2, $3, $4, $5, $6, $8, $9, $12, $13, $14
}' "$TMP"
echo "per-run : $TMP"
echo "cumulative: $OUT_TSV"
