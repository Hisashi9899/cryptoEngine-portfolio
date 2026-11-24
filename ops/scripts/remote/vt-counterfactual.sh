#!/usr/bin/env bash
# V.T コストゲート反実仮想（読み取り専用・発注経路なし）
#
# 目的: 「コストゲートが棄却した取引は、実際に儲かっていたのか」を測る。
#   live は --cost-gate-k=2.5 で運用中。同じ板データを k=2.5 / 1.0 / 0 で
#   リプレイし、同一の入口・出口・deny・RSI下限で採点して比較する。
#   k を下げても PF が改善しないなら、ゲートは正しく機能している。
#
# 安全設計（稼働中の売買には一切触れない）:
#   - vt-replay は発注経路を持たない（板 CSV を読むだけ）。
#   - 実行は独立した一時コンテナ（--cpus=1 --memory=192m）。稼働中の
#     core / vtlive の cgroup には入らないため、OOM・CPU飢餓を起こさない。
#   - 出力は <trade_logs>/cf/ 配下のみ。live/paper が読む
#     vt_signals_*.jsonl とはディレクトリが別（読み込みは非再帰 glob）。
#   - 手数料は taker/taker（0.05%/片道）で統一。ゲートのコスト前提と一致。
#   - balance=100000 で数量0（サイジング由来の幽霊玉）を減らし、ゲートの
#     効果を分離する。実運用の 5,000 円では最小数量に満たない買いが増える
#     ため、その差はレポートで別途言及する。
#
# 使い方:
#   bash ops/scripts/remote/vt-counterfactual.sh
#   VT_CF_DAYS=7 VT_CF_SYMBOLS="BCH" VT_CF_KS="1.0" bash ...   # 部分実行
#
# 出力:
#   <trade_logs>/cf/<run_id>/k<k>/vt_signals_*.jsonl  （採点対象の記録）
#   ops/logs/vt-counterfactual.tsv                   （累積の比較表）
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
LOG_DIR="${VT_CF_LOG_DIR:-$REPO_ROOT/backend/core/trade_logs}"
OUT_ROOT="${VT_CF_OUT_ROOT:-$LOG_DIR/cf}"
SUMMARY="${VT_CF_SUMMARY:-$REPO_ROOT/ops/logs/vt-counterfactual.tsv}"
IMAGE="${VT_CF_IMAGE:-cryptoengine-core:latest}"
ENV_FILE="${VT_CF_ENV_FILE:-$REPO_ROOT/infra/config/.env.prod}"
SYMBOLS="${VT_CF_SYMBOLS:-BCH LINK LTC}"
KS="${VT_CF_KS:-2.5 1.0 0}"
DAYS="${VT_CF_DAYS:-14}"
BALANCE="${VT_CF_BALANCE:-100000}"
RISK="${VT_CF_RISK:-0.2}"
BARS="${VT_CF_BARS:-300}"
DENY_FILE="${VT_CF_DENY_FILE:-/trade_logs/vt_static_deny.json}"

log() { printf '%s %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"; }

if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
  log "ERROR: image not found: $IMAGE"
  exit 1
fi
if [ ! -r "$ENV_FILE" ]; then
  log "ERROR: env file not readable: $ENV_FILE"
  exit 1
fi
if [ ! -d "$LOG_DIR" ]; then
  log "ERROR: trade_logs not found: $LOG_DIR"
  exit 1
fi
if [ ! -r "$LOG_DIR/vt_static_deny.json" ]; then
  log "ERROR: static deny file missing: $LOG_DIR/vt_static_deny.json"
  exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
  log "ERROR: jq not found"
  exit 1
fi

FROM="${VT_CF_FROM:-$(date -d "$DAYS days ago" +%Y%m%d)}"
TO="${VT_CF_TO:-$(date +%Y%m%d)}"
RUN_ID="$(date +%Y%m%d_%H%M%S)"
mkdir -p "$OUT_ROOT/$RUN_ID" "$(dirname "$SUMMARY")"

# コンテナ内の相対パスは OUT_ROOT から導く（cf / cf-deny 等）。
case "$OUT_ROOT" in
  "$LOG_DIR"/*) REL_ROOT="${OUT_ROOT#"$LOG_DIR"/}" ;;
  *)
    log "ERROR: VT_CF_OUT_ROOT must be under $LOG_DIR (got: $OUT_ROOT)"
    exit 1 ;;
esac

DOCKER_ARGS=(--rm --cpus=1 --memory=192m
  --env-file "$ENV_FILE"
  -e ORDERBOOK_LOG_DIR=/trade_logs
  -v "$LOG_DIR:/trade_logs")

if [ ! -s "$SUMMARY" ]; then
  printf 'run_id\tfrom\tto\tbalance\tsymbol\tk\tpairs\twins\tpf\tci_low\tci_high\tnet\n' >"$SUMMARY"
fi

# 板CSVの被覆を数える。1KB未満はヘッダのみ＝データなしとみなす。
# 欠測日を「0件で成功」と誤認しないためのガード（GMO ws 停止時など）。
coverage() {
  local sym="$1" d="$FROM" n=0 total=0 f sz
  while [ "$d" -le "$TO" ]; do
    total=$((total + 1))
    f="$LOG_DIR/orderbook_${sym}_${d}.csv"
    [ -s "$f" ] || f="$LOG_DIR/orderbook_${sym}_${d}.csv.gz"
    if [ -s "$f" ]; then
      sz="$(stat -c%s "$f" 2>/dev/null || echo 0)"
      if [ "$sz" -gt 1024 ]; then
        n=$((n + 1))
      fi
    fi
    d="$(date -d "$d +1 day" +%Y%m%d)"
  done
  printf '%s %s' "$n" "$total"
}

log "counterfactual start run=$RUN_ID range=$FROM..$TO balance=$BALANCE k=[$KS] symbols=[$SYMBOLS]"

for sym in $SYMBOLS; do
  read -r cov_n cov_total <<<"$(coverage "$sym")"
  if [ "$cov_n" -eq 0 ]; then
    log "WARN: no orderbook rows sym=$sym range=$FROM..$TO (ws outage? result is not evidence)"
  else
    log "coverage sym=$sym days_with_data=$cov_n/$cov_total"
  fi
  for k in $KS; do
    rel="$REL_ROOT/$RUN_ID/$sym/k$k"
    host_dir="$OUT_ROOT/$RUN_ID/$sym/k$k"
    mkdir -p "$host_dir"
    out_name="vt_signals_CF${sym}_${BARS}_00000000.jsonl"

    if ! docker run "${DOCKER_ARGS[@]}" --entrypoint core "$IMAGE" \
      vt-replay --symbol="$sym" --bars="$BARS" --from="$FROM" --to="$TO" \
      --cost-gate-k="$k" --cost-gate-fee-mode=taker \
      --short-rsi-floor=40 --min-confidence=0 \
      --deny-file="$DENY_FILE" \
      --trail-activate=0.015 --trail-back=0.008 \
      --exit-confirm-votes=2 --entry-cooldown-sec=300 \
      --balance-jpy="$BALANCE" --risk-fraction="$RISK" \
      --out="/trade_logs/$rel/$out_name" \
      >"$host_dir/replay.log" 2>&1; then
      log "WARN: replay failed sym=$sym k=$k (see $host_dir/replay.log)"
      continue
    fi

    score="$(docker run "${DOCKER_ARGS[@]}" --entrypoint core "$IMAGE" \
      vt-acceptance --dir="/trade_logs/$rel" --bars="$BARS" --days=0 \
      --symbols="$sym" --fee-mode=taker 2>/dev/null | tail -1)"
    if [ -z "$score" ]; then
      log "WARN: score empty sym=$sym k=$k"
      continue
    fi

    pairs="$(printf '%s' "$score" | jq -r '.pairs // 0')"
    wins="$(printf '%s' "$score" | jq -r '.wins // 0')"
    pf="$(printf '%s' "$score" | jq -r '.pf // 0')"
    lo="$(printf '%s' "$score" | jq -r '.pf_ci_low // 0')"
    hi="$(printf '%s' "$score" | jq -r '.pf_ci_high // 0')"
    net="$(printf '%s' "$score" | jq -r '.net // 0')"
    printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
      "$RUN_ID" "$FROM" "$TO" "$BALANCE" "$sym" "$k" \
      "$pairs" "$wins" "$pf" "$lo" "$hi" "$net" >>"$SUMMARY"
    log "done sym=$sym k=$k pairs=$pairs wins=$wins pf=$pf ci=[$lo,$hi] net=$net"
  done
done

echo
echo "=== vt counterfactual $RUN_ID ($FROM..$TO, balance=$BALANCE, fee=taker) ==="
printf '%-5s %-4s %6s %6s %8s %9s %9s %12s\n' SYM K PAIRS WINS PF CI_LOW CI_HIGH NET
awk -F'\t' -v id="$RUN_ID" '$1==id {
  printf "%-5s %-4s %6s %6s %8.3f %9.3f %9.3f %12.1f\n", $5, $6, $7, $8, $9, $10, $11, $12
}' "$SUMMARY"
echo "summary: $SUMMARY"
echo "data coverage (days with rows / range days):"
for sym in $SYMBOLS; do
  read -r cov_n cov_total <<<"$(coverage "$sym")"
  printf '  %-5s %s/%s\n' "$sym" "$cov_n" "$cov_total"
done
log "counterfactual end run=$RUN_ID"
