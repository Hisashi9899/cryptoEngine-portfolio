#!/usr/bin/env bash
# static deny の ablation（読み取り専用・発注経路なし）。
#
#   bash ops/scripts/remote/vt-deny-ablation.sh
#
# 目的: live が常用している static deny（vt_static_deny.json の4パターン）が
#   実際に成績を改善しているのかを、deny 無しとの同一窓リプレイで測る。
#   in-sample の負けパターンが out-of-sample で勝ちに転じた実績（2026-11-20）
#   があるため、「deny が効いている」という前提自体を検証する。
#
# 安全設計:
#   - vt-replay は発注経路を持たない。独立一時コンテナ（1CPU/192MB）。
#   - live の vt_static_deny.json は読むだけで変更しない。
#   - 空denyは <trade_logs>/vt_cf_nodeny.json（CF専用・一時ファイル）。
#   - 出力は <trade_logs>/cf-nodeny/ のみ。
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
LOG_DIR="${VT_ABL_LOG_DIR:-$REPO_ROOT/backend/core/trade_logs}"
EMPTY_DENY="${VT_ABL_EMPTY_DENY:-$LOG_DIR/vt_cf_nodeny.json}"
FROM="${VT_ABL_FROM:-20261108}"
TO="${VT_ABL_TO:-20261120}"
KS="${VT_ABL_KS:-0 1.0}"
SYMBOLS="${VT_ABL_SYMBOLS:-BCH LINK LTC}"
RUN_CONTROL="${VT_ABL_RUN_CONTROL:-1}"

log() { printf '%s %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"; }

# 空deny（全許可）。形式は vt_static_deny.json と同じ。
cat >"$EMPTY_DENY" <<JSON
{
  "date": "20261120",
  "days": 14,
  "bars_sec": 300,
  "min_pairs": 5,
  "blocked_long": [],
  "blocked_short": [],
  "allowed": []
}
JSON
log "wrote empty deny: $EMPTY_DENY"

run_cf() {
  local outroot="$1" summary="$2" deny="$3"
  env VT_CF_FROM="$FROM" VT_CF_TO="$TO" \
      VT_CF_OUT_ROOT="$LOG_DIR/$outroot" \
      VT_CF_SUMMARY="$REPO_ROOT/ops/logs/$summary" \
      VT_CF_DENY_FILE="$deny" \
      VT_CF_KS="$KS" \
      VT_CF_SYMBOLS="$SYMBOLS" \
    bash "$REPO_ROOT/ops/scripts/remote/vt-counterfactual.sh"
}

if [ "$RUN_CONTROL" = "1" ]; then
  log "control start (static deny) $FROM..$TO"
  run_cf cf-deny vt-counterfactual-deny.tsv /trade_logs/vt_static_deny.json \
    || log "WARN: control run failed"
else
  log "control skipped (VT_ABL_RUN_CONTROL=0); reusing existing cf-deny run"
fi

log "ablation start (no deny) $FROM..$TO"
run_cf cf-nodeny vt-counterfactual-nodeny.tsv /trade_logs/vt_cf_nodeny.json \
  || log "WARN: ablation run failed"

echo
echo "=== static deny ablation $FROM..$TO (fee=taker) ==="
for f in vt-counterfactual-deny.tsv vt-counterfactual-nodeny.tsv; do
  echo "-- $f --"
  awk -F'\t' -v from="$FROM" -v to="$TO" \
    'NR>1 && $2==from && $3==to {printf "  %-4s k%-4s pairs=%-3s wins=%-3s pf=%-9.3f ci=[%-6.3f,%-7.3f] net=%9.1f\n",$5,$6,$7,$8,$9,$10,$11,$12}' \
    "$REPO_ROOT/ops/logs/$f"
done
log "deny-ablation end"
