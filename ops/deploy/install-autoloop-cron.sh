#!/usr/bin/env bash
# 永続進化ループ cron の冪等インストーラ（VPS 上で実行）。
#
#   bash /opt/cryptoengine/ops/deploy/install-autoloop-cron.sh [--apply]
#
# 既定は dry-run（追加される行のみ表示）。--apply で crontab へ反映。
# マーカー行で重複追記を防止する。既存の capture/paper/rotate/calib には触れない。
set -u

MARK_BEGIN="# BEGIN crypto-autoloop"
MARK_END="# END crypto-autoloop"
SNIPPET_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SNIPPET="$SNIPPET_DIR/cron-autoloop.txt"
TMP=/tmp/cron-autoloop.new

if [ ! -f "$SNIPPET" ]; then
  echo "snippet not found: $SNIPPET" >&2
  exit 1
fi

CURRENT="$(crontab -l 2>/dev/null || true)"
# 既存ブロックを除去してから付け直す（冪等）
FILTERED="$(printf '%s\n' "$CURRENT" | awk -v b="$MARK_BEGIN" -v e="$MARK_END" '$0==b{p=1;next} $0==e{p=0;next} !p')"
{
  printf '%s\n' "$FILTERED"
  printf '%s\n' "$MARK_BEGIN"
  grep -v '^$' "$SNIPPET" | grep -v "$MARK_BEGIN" | grep -v "$MARK_END" || true
  printf '%s\n' "$MARK_END"
} | sed '/^$/N;/^\n$/D' > "$TMP"

echo "--- lines to install ---"
grep -A100 "$MARK_BEGIN" "$TMP" | grep -B100 "$MARK_END" || true

if [ "${1:-}" = "--apply" ]; then
  crontab "$TMP"
  echo applied
  crontab -l | grep -c "crypto-autoloop\|vt-tune\|pf-gate\|vt-weekly-report" || true
fi
