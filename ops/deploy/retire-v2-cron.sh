#!/usr/bin/env bash
# v1/v2/v3 退役に伴う cron 整理（VPS 上で実行）。
#
#   bash /opt/cryptoengine/ops/deploy/retire-v2-cron.sh [--apply]
#
# やること（冪等・コメントアウトのみで削除しない）:
#   - v2 由来の定期実行を無効化: paper(5分) / weekly-calib(日曜) / shadow(日曜) / pcalib(日曜)
#   - V.T週次採点ブロックを追加（日曜 07:05、vt-score → vt_score_weekly.json）
# 残す: capture系 / vt-paper / funding / watchdog / prune / rotate
set -u

MARK_BEGIN="# BEGIN crypto-vtscore-weekly"
MARK_END="# END crypto-vtscore-weekly"
TMP=/tmp/cron-retire-v2.new

CURRENT="$(crontab -l 2>/dev/null || true)"
if [ -z "$CURRENT" ]; then
  echo "crontab is empty; nothing to do" >&2
  exit 1
fi

# 1) v2 行のコメントアウト（既に # 始まりの行は触らない）
FILTERED="$(printf '%s\n' "$CURRENT" | sed \
  -e '/^#/! s|^\(.*crypto-paper\.lock.*\)$|# RETIRED 2026-09-22 v1v2v3: \1|' \
  -e '/^#/! s|^\(.*crypto-calib\.lock.*\)$|# RETIRED 2026-09-22 v1v2v3: \1|' \
  -e '/^#/! s|^\(.*crypto-shadow\.lock.*\)$|# RETIRED 2026-09-22 v1v2v3: \1|' \
  -e '/^#/! s|^\(.*crypto-pcalib\.lock.*\)$|# RETIRED 2026-09-22 v1v2v3: \1|')"

# 2) vt-score ブロック（既存なら除去して付け直し）
NOBLOCK="$(printf '%s\n' "$FILTERED" | awk -v b="$MARK_BEGIN" -v e="$MARK_END" '$0==b{p=1;next} $0==e{p=0;next} !p')"
{
  printf '%s\n' "$NOBLOCK"
  printf '%s\n' "$MARK_BEGIN"
  printf '%s\n' "# V.T週次採点（日曜 07:05）。vt_signals を集計し pull 対象にする。"
  printf '%s\n' "5 7 * * 0 /usr/bin/flock -n /tmp/crypto-vtscore.lock /usr/bin/nice -n 10 /usr/bin/docker compose -f /opt/cryptoengine/infra/docker/docker-compose.yml -f /opt/cryptoengine/infra/docker/docker-compose.prod.yml exec -T core core vt-score --dir=/trade_logs --days=7 --src=signals --out=/trade_logs/vt_score_weekly.json >> /opt/cryptoengine/ops/logs/cron-vtscore.log 2>&1"
  printf '%s\n' "$MARK_END"
} | sed '/^$/N;/^\n$/D' > "$TMP"

echo "--- retired lines ---"
grep -c "^# RETIRED 2026-09-22" "$TMP" || true
grep "^# RETIRED 2026-09-22" "$TMP" | sed 's/ --.*//' || true
echo "--- vtscore block ---"
grep -A3 "$MARK_BEGIN" "$TMP" | head -n 5 || true

if [ "${1:-}" = "--apply" ]; then
  crontab "$TMP"
  echo applied
fi
