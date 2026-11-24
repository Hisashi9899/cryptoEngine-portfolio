#!/usr/bin/env bash
# cryptoEngine ops — pull logs from the VPS to the local WSL box.
#
#   ops/scripts/collect.sh
#
# Flow:
#   1) snapshot 24h of docker / nginx / system / db logs ON the VPS (collect only)
#   2) rsync over SSH (pull) into ops/logs/YYYYMMDD/
#   3) verify MANIFEST.sha256, mark missing datasets (sweep input exclusion)
#   4) prune VPS hot files older than RETAIN_DAYS *only if* manifest verified
#      (VPS 保持量を bound する。未回収データの削除は構造的に起きない)
#
# Requires: config.env, ssh key, collect user on the VPS.
set -euo pipefail

OPS_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG="$OPS_ROOT/scripts/config.env"
if [ ! -f "$CONFIG" ]; then
  echo "config.env not found — cp scripts/config.env.example scripts/config.env and fill it in." >&2
  exit 1
fi
set -a; source "$CONFIG"; set +a

# VPS 完結ポリシー（ops/PRODUCT_DATA_POLICY.md）: bulk pull は廃止。
# 成果物のみ持ち帰る。通常は ops/deploy/sync-master.sh pull を使うこと。
# 旧来の bulk 収集がどうしても要る場合のみ BULK_PULL=1 で実行する（非推奨）。
if [ "${BULK_PULL:-0}" != "1" ]; then
  echo "bulk pull は廃止（VPS 完結ポリシー）。成果物のみ取得する。"
  echo "使うコマンド: ops/deploy/sync-master.sh pull [--db]"
  exec "$OPS_ROOT/deploy/sync-master.sh" pull "$@"
fi
echo "WARNING: BULK_PULL=1（非推奨）。ローカルに raw を溜めないこと。" >&2

SSH_DEST="${COLLECT_USER}@${VPS_HOST}"
SSH_OPTS="-i $SSH_KEY -p $SSH_PORT -o BatchMode=yes -o StrictHostKeyChecking=accept-new"
DATE="$(date +%Y%m%d)"
LOCAL_DIR="$OPS_ROOT/logs/$DATE"

echo "==> collecting $DATE from $VPS_HOST"
mkdir -p "$LOCAL_DIR"

echo "==> (1/2) snapshotting on the VPS"
ssh $SSH_OPTS "$SSH_DEST" \
  "STAGING_DIR='$STAGING_DIR' COMPOSE_DIR='$COMPOSE_DIR' COMPOSE_FILE='$COMPOSE_FILE' NGINX_ACCESS_LOG='$NGINX_ACCESS_LOG' NGINX_ERROR_LOG='$NGINX_ERROR_LOG' DATE='$DATE' bash -s" \
  < "$OPS_ROOT/scripts/remote/dump-remote.sh"

echo "==> (2/2) pulling via rsync over SSH"
rsync -az -e "ssh $SSH_OPTS" \
  "$SSH_DEST:$STAGING_DIR/$DATE/" "$LOCAL_DIR/"

echo "==> (3/3) verifying manifest and marking missing datasets"
MISSING="$LOCAL_DIR/MISSING.txt"
: > "$MISSING"
if [ -f "$LOCAL_DIR/MANIFEST.sha256" ]; then
  if (cd "$LOCAL_DIR" && sha256sum -c MANIFEST.sha256 > /dev/null 2>&1); then
    echo "manifest: OK"
  else
    echo "manifest: MISMATCH (see below)" | tee -a "$MISSING"
    (cd "$LOCAL_DIR" && sha256sum -c MANIFEST.sha256 2>&1 | grep -v ': OK$' || true) | tee -a "$MISSING"
  fi
else
  echo "manifest: absent (old dump-remote.sh?)" | tee -a "$MISSING"
fi
# sweep 入力に必須の実データをチェック。欠落は除外マークし、推測で埋めない。
if ! ls "$LOCAL_DIR"/market/orderbook_*.csv >/dev/null 2>&1; then
  echo "missing: market/orderbook_*.csv (sweep-v2 input unavailable)" | tee -a "$MISSING"
fi
if [ ! -f "$LOCAL_DIR/db/tables.sql" ] || ! grep -qE "COPY |INSERT INTO " "$LOCAL_DIR/db/tables.sql" 2>/dev/null; then
  echo "missing: db/tables.sql rows (calibration teacher data unavailable)" | tee -a "$MISSING"
fi
if [ ! -s "$MISSING" ]; then
  echo "complete" > "$MISSING"
fi

RETAIN_DAYS="${RETAIN_DAYS:-30}"
echo "==> (4/4) pruning VPS hot files older than $RETAIN_DAYS days (manifest-OK only)"
# MANIFEST 検証済みのファイルだけがここに存在する。よって
# 「ローカルにある古い日付ファイル == 回収検証済み」であり、
# その VPS 側同名ファイルの削除は安全（未回収データ消失は起きない）。
# 保持期間の真実源はローカル側のこの値（VPS cron とは独立）。
MANIFEST_OK=0
if [ -f "$LOCAL_DIR/MANIFEST.sha256" ] \
  && (cd "$LOCAL_DIR" && sha256sum -c MANIFEST.sha256 > /dev/null 2>&1); then
  MANIFEST_OK=1
fi
if [ "$MANIFEST_OK" = "1" ] && [ -f "$LOCAL_DIR/vars.env" ]; then
  # shellcheck disable=SC1090
  ORDERBOOK_LOG_DIR=""; source "$LOCAL_DIR/vars.env"
  if [ -n "${ORDERBOOK_LOG_DIR:-}" ]; then
    CUTOFF="$(date -d "$RETAIN_DAYS days ago" +%Y%m%d)"
    PRUNE_LIST="$(mktemp)"
    for f in "$LOCAL_DIR"/market/orderbook_*.csv \
             "$LOCAL_DIR"/trading/signals_v2_*.jsonl \
             "$LOCAL_DIR"/trading/calibration_*.json; do
      [ -e "$f" ] || continue
      base="$(basename "$f")"
      # ファイル名内の YYYYMMDD を抽出（orderbook_YYYYMMDD.csv 等）。
      day="$(printf '%s' "$base" | grep -oE '[0-9]{8}' | head -1 || true)"
      if [ -n "$day" ] && [ "$day" \< "$CUTOFF" ]; then
        printf '%s/%s\n' "$ORDERBOOK_LOG_DIR" "$base" >> "$PRUNE_LIST"
      fi
    done
    if [ -s "$PRUNE_LIST" ]; then
      echo "pruning VPS files older than $CUTOFF:"
      cat "$PRUNE_LIST"
      # xargs -0 ではなく行単位（ファイル名に空白・改行は想定しない）。
      # shellcheck disable=SC2029
      ssh $SSH_OPTS "$SSH_DEST" "xargs rm -f < /dev/stdin" < "$PRUNE_LIST"
      echo "pruned."
    else
      echo "nothing to prune (all within $RETAIN_DAYS days)."
    fi
    rm -f "$PRUNE_LIST"
  else
    echo "WARNING: vars.env without ORDERBOOK_LOG_DIR — skipping prune (fail-safe)." >&2
  fi
else
  echo "SKIP prune: manifest not verified — keeping all VPS files (fail-safe)." >&2
fi

echo "==> saved to $LOCAL_DIR"
find "$LOCAL_DIR" -type f | sort
