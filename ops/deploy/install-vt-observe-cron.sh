#!/usr/bin/env bash
# V.T dry-run 観測点通知 cron の冪等インストーラ（VPS 上で実行）。
#
#   bash /opt/cryptoengine/ops/deploy/install-vt-observe-cron.sh [--apply]
#
# 既定は dry-run（反映後の行のみ表示）。--apply で crontab へ反映。
# マーカーブロック内だけを入れ替えるため、他ジョブには触れない。
# 手動追加された同一行（マーカー外）も重複しないよう除去する。
set -u

MARK_BEGIN="# BEGIN crypto-vt-observe"
MARK_END="# END crypto-vt-observe"
SNIPPET_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SNIPPET="$SNIPPET_DIR/cron-vt-observe.txt"
TMP=/tmp/cron-vt-observe.new

if [ ! -f "$SNIPPET" ]; then
  echo "snippet not found: $SNIPPET" >&2
  exit 1
fi

strip_block() {
  awk -v b="$1" -v e="$2" '$0==b{p=1;next} $0==e{p=0;next} !p'
}

CURRENT="$(crontab -l 2>/dev/null || true)"
FILTERED="$(printf '%s\n' "$CURRENT" \
  | strip_block "$MARK_BEGIN" "$MARK_END" \
  | grep -v 'vt_observe_due\.py' || true)"

{
  printf '%s\n' "$FILTERED"
  printf '%s\n' "$MARK_BEGIN"
  grep -v '^#' "$SNIPPET" | grep -v '^$' || true
  printf '%s\n' "$MARK_END"
} | sed '/^$/N;/^\n$/D' >"$TMP"

echo "--- observe lines after install (want 1) ---"
grep -c "vt_observe_due.py" "$TMP" || true
echo "--- total crontab lines after install ---"
wc -l <"$TMP"

if [ "${1:-}" = "--apply" ]; then
  crontab "$TMP"
  echo applied
else
  echo "dry-run (use --apply to install)"
fi
