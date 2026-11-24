#!/usr/bin/env bash
# V.T全量記録 cron の冪等インストーラ（VPS 上で実行）。
#
#   bash /opt/cryptoengine/ops/deploy/install-vt-cron.sh [--apply]
#
# 既定は dry-run（追加される行のみ表示）。--apply で crontab へ反映。
# 旧ブロック（crypto-vt-tf / crypto-vt-17）は除去し、新ブロック
# （crypto-vt-all: 17銘柄×3足種を17プロセス）に入れ替える。
# 5分足の旧5行（無印）は別管理のため残す（重複時は flock で排他されるが、
# 旧ファイル名と新ファイル名は別物のため、適用後に手で消すこと。詳細は下記）。
set -u

MARK_BEGIN="# BEGIN crypto-vt-all"
MARK_END="# END crypto-vt-all"
SNIPPET_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SNIPPET="$SNIPPET_DIR/cron-vt-all.txt"
TMP=/tmp/cron-vt-all.new

if [ ! -f "$SNIPPET" ]; then
  echo "snippet not found: $SNIPPET" >&2
  exit 1
fi

strip_block() {
  awk -v b="$1" -v e="$2" '$0==b{p=1;next} $0==e{p=0;next} !p'
}

CURRENT="$(crontab -l 2>/dev/null || true)"
# 旧5分足5行（無印ロック crypto-vt-btc 等）は新17行に統合のため無効化する。
# 新ロック（vtall/vt1m/vt1h）には触れない。
FILTERED="$(printf '%s\n' "$CURRENT" | sed \
  -e '/^#/! s|^\(.*crypto-vt-btc\.lock.*\)$|# SUPERSEDED by crypto-vt-all: \1|' \
  -e '/^#/! s|^\(.*crypto-vt-xrp\.lock.*\)$|# SUPERSEDED by crypto-vt-all: \1|' \
  -e '/^#/! s|^\(.*crypto-vt-xlm\.lock.*\)$|# SUPERSEDED by crypto-vt-all: \1|' \
  -e '/^#/! s|^\(.*crypto-vt-bch\.lock.*\)$|# SUPERSEDED by crypto-vt-all: \1|' \
  -e '/^#/! s|^\(.*crypto-vt-ltc\.lock.*\)$|# SUPERSEDED by crypto-vt-all: \1|' \
  | strip_block "# BEGIN crypto-vt-tf" "# END crypto-vt-tf" \
  | strip_block "# BEGIN crypto-vt-17" "# END crypto-vt-17" \
  | strip_block "$MARK_BEGIN" "$MARK_END")"
{
  printf '%s\n' "$FILTERED"
  printf '%s\n' "$MARK_BEGIN"
  grep -v '^#' "$SNIPPET" | grep -v '^$' || true
  printf '%s\n' "$MARK_END"
} | sed '/^$/N;/^\n$/D' > "$TMP"

echo "--- vt-paper lines after install ---"
grep -c "vt-paper --symbol" "$TMP" || true

if [ "${1:-}" = "--apply" ]; then
  crontab "$TMP"
  echo applied
fi
