#!/usr/bin/env bash
# バッチ cron に nice を付与し、trader（5秒 loop）をCPU優先にする。
# 取得・paper・sweep・較正・prune・watchdog は I/O 待ち中心のため nice -n 10 でも
# 実害なし。trader 行（auto-loop 常駐プロセス）には触れない。
#
# 使い方（VPS 上）:
#   bash nice-batch-cron.sh           変更件数の表示のみ（dry-run）
#   bash nice-batch-cron.sh --apply   crontab へ反映
#
# 冪等: 既に nice 付きの行は置換しない。復旧は nice 部分を手で外すだけ。
set -u

TMP=/home/app/cron.new
crontab -l 2>/dev/null | sed \
  -e '/nice -n 10/!s|/usr/bin/flock|/usr/bin/nice -n 10 /usr/bin/flock|' \
  -e '/nice -n 10/!s|/usr/bin/python3 /home|/usr/bin/nice -n 10 /usr/bin/python3 /home|' > "$TMP"
echo "nice lines: $(grep -c 'nice -n 10' "$TMP")"
echo "total lines: $(grep -c -v '^#\|^$' "$TMP")"
if [ "${1:-}" = "--apply" ]; then
  crontab "$TMP"
  echo applied
fi
