#!/usr/bin/env bash
# FR収集 cron の冪等インストーラ（VPS 上で実行）。
#
#   bash /opt/cryptoengine/ops/deploy/install-funding-cron.sh [--apply]
#
# 既定は dry-run（追加される行のみ表示）。--apply で crontab へ反映。
# マーカー行で重複追記を防止する。既存の capture/paper/rotate/calib には触れない。
set -u

MARK_BEGIN="# BEGIN crypto-funding-collect"
MARK_END="# END crypto-funding-collect"
SNIPPET_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SNIPPET="$SNIPPET_DIR/cron-funding.txt"
TMP=/tmp/cron-funding.new

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
  grep -v '^# インストール' "$SNIPPET" | grep -v '^# 仕様' | grep -v '^# 保存先' | grep -v '^$' || true
  printf '%s\n' "$MARK_END"
} | sed '/^$/N;/^\n$/D' > "$TMP"

echo "--- lines to install ---"
grep -A100 "$MARK_BEGIN" "$TMP" | grep -B100 "$MARK_END" || true
echo "total non-comment lines: $(grep -c -v '^#\|^$' "$TMP")"

if [ "${1:-}" = "--apply" ]; then
  crontab "$TMP"
  echo applied
  crontab -l | grep -c "funding_collect" || true
fi
