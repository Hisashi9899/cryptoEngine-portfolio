#!/usr/bin/env bash
# 週次較正ジョブ（VPS・host 側で実行）。
#
# 直近7日分のBTC現物板を連結 → sweep-v2 → 適格時のみ calibration_latest.json
# を更新する。単日では発火が0〜1ペアで適格（20件）に届かず較正が一生育たない
# ため、窓を7日に広げ適格を10件に緩める。連結CSV内の日次ヘッダ行は
# backtest の parseRow が安全にスキップする（整数でない timestamp は無視）。
#
# fail-safe: 入力不足・非適格・sweep 失敗の日は旧ファイルを温存し、何も壊さない。
# trader は mtime 変化で自動再読込する（再起動不要）。
#
# 使い方:
#   bash weekly-calib.sh                 本番実行（日曜 cron 用）
#   DRY_RUN=1 bash weekly-calib.sh       検証実行（/trade_logs に _dryrun 付きで書き出し、
#                                        calibration_latest.json には触れない）
#   bash weekly-calib.sh --install       日曜 cron を本スクリプト呼び出しに置換
set -u

REPO=/opt/cryptoengine
SRC=$REPO/backend/core/trade_logs
C=/trade_logs
DAYS="${DAYS:-7}"
MIN_PAIRS="${MIN_PAIRS:-10}"
WEEK_HOST=$SRC/orderbook_BTC_week_tmp.csv
WEEK_C=$C/orderbook_BTC_week_tmp.csv
SWEEP_OUT=$C/sweep_calib_week.json
CAND_HOST=$SRC/calibration_weekly.json
CAND_C=$C/calibration_weekly.json
LATEST_HOST=$SRC/calibration_latest.json
if [ "${DRY_RUN:-0}" = "1" ]; then
  SWEEP_OUT=$C/sweep_calib_week_dryrun.json
  CAND_HOST=$SRC/calibration_weekly_dryrun.json
  CAND_C=$C/calibration_weekly_dryrun.json
fi

log() { echo "[weekly-calib] $(date -u +%FT%TZ) $*"; }

if [ "${1:-}" = "--install" ]; then
  crontab -l 2>/dev/null | grep -v 'crypto-calib.lock' > /home/app/cron.new
  printf '%s\n' "10 6 * * 0 /usr/bin/flock -n /tmp/crypto-calib.lock bash $REPO/ops/scripts/remote/weekly-calib.sh >> $REPO/ops/logs/cron-calib.log 2>&1" >> /home/app/cron.new
  # shadow 監査（日曜 07:00、較正の後）。run --rm のため稼働コンテナに影響なし。
  # 出力は上書き（shadow_audit_weekly.json）。grep で冪等化する。
  if ! grep -q 'shadow_audit_weekly' /home/app/cron.new; then
    printf '%s\n' "0 7 * * 0 /usr/bin/flock -n /tmp/crypto-shadow.lock /usr/bin/nice -n 10 /usr/bin/docker compose -f $REPO/infra/docker/docker-compose.yml -f $REPO/infra/docker/docker-compose.prod.yml run --rm core shadow-audit --shadow=/trade_logs/shadow_edge.jsonl --csv-dir=/trade_logs --out=/trade_logs/shadow_audit_weekly.json >> $REPO/ops/logs/cron-shadow.log 2>&1" >> /home/app/cron.new
  fi
  # paper 擬似ラベル還流（日曜 07:30）。live 約定を待たず較正を育てる。
  # base=latest 自体への書き戻しで累積する。grep で冪等化する。
  # --per-symbol-dir で銘柄別較正（calib_sym/calibration_<SYM>.json）も育てる。
  # trader は V2_CALIB_DIR で新しい方を自動採用する（再起動不要）。
  if ! grep -q 'paper-calib' /home/app/cron.new; then
    printf '%s\n' "30 7 * * 0 /usr/bin/flock -n /tmp/crypto-pcalib.lock /usr/bin/nice -n 10 /usr/bin/docker compose -f $REPO/infra/docker/docker-compose.yml -f $REPO/infra/docker/docker-compose.prod.yml run --rm core paper-calib --signals-dir=/trade_logs --csv-dir=/trade_logs --base=/trade_logs/calibration_latest.json --out=/trade_logs/calibration_latest.json --per-symbol-dir=/trade_logs/calib_sym >> $REPO/ops/logs/cron-pcalib.log 2>&1" >> /home/app/cron.new
  fi
  crontab /home/app/cron.new
  log "cron installed"
  exit 0
fi

cleanup() { rm -f "$WEEK_HOST" "$SRC/sweep_calib_week_dryrun.json"* "$SRC/calibration_weekly_dryrun.json"; }
trap cleanup EXIT

COUNT=0
i=1
rm -f "$WEEK_HOST" "$CAND_HOST"
: > "$WEEK_HOST"
while [ "$i" -le "$DAYS" ]; do
  day="$(date -d "$i days ago" +%Y%m%d)"
  # .csv 優先、無ければ .csv.gz（prune が gzip 化する）。
  # zcat -f は両形式を透過的に連結する。
  f="$SRC/orderbook_BTC_${day}.csv"
  [ -f "$f" ] || f="$SRC/orderbook_BTC_${day}.csv.gz"
  if [ -f "$f" ]; then
    zcat -f "$f" >> "$WEEK_HOST"
    COUNT=$((COUNT + 1))
  fi
  i=$((i + 1))
done
if [ "$COUNT" -lt 3 ]; then
  log "abort: only $COUNT daily files (need >=3); keeping old calibration"
  exit 0
fi
log "concat $COUNT daily files"

cd "$REPO/infra/docker"
if docker compose -f docker-compose.yml -f docker-compose.prod.yml exec -T core core sweep-v2 "$WEEK_C" --fee-bps=0.0005 --min-pairs="$MIN_PAIRS" --cal-out="$CAND_C" --out="$SWEEP_OUT"; then
  if [ -f "$CAND_HOST" ]; then
    if [ "${DRY_RUN:-0}" = "1" ]; then
      log "dryrun: candidate ready (eligible run exists)"
    else
      # 累積マージ：上書きでなく育てる。base 不在時は candidate が seed になる。
      if docker compose -f docker-compose.yml -f docker-compose.prod.yml exec -T core core calib-merge --base="$C/calibration_latest.json" --add="$CAND_C" --out="$C/calibration_latest.json"; then
        log "merged candidate into calibration_latest.json"
      else
        log "merge failed; keeping old calibration"
      fi
    fi
  else
    log "no eligible run; keeping old calibration"
  fi
else
  log "sweep failed; keeping old calibration"
fi
