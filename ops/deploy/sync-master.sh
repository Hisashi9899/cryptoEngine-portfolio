#!/usr/bin/env bash
# マスターVPS ↔ WSL 同期（WSL で実行）
#
#   ops/deploy/sync-master.sh pull [--db]        VPS → WSL（成果物の回収）
#   ops/deploy/sync-master.sh push [--dry-run]   WSL → VPS（再デプロイ）
#
# pull 対象（VPS 完結ポリシー。raw の bulk 回収は廃止。詳細は ops/PRODUCT_DATA_POLICY.md）:
#   - backend/core/trade_logs/trade_log.csv          （監査証跡）
#   - backend/core/trade_logs/calibration_latest.json （較正の成果物）
#   - backend/core/trade_logs/trader_heartbeat.json   （稼働証跡）
#   - backend/core/trade_logs/funding.jsonl           （FR収集の成果物・反転検出用）
#   - backend/core/trade_logs/vt_score_weekly.json    （V.T週次採点の成果物）
# 板 CSV・signals jsonl・cron 生ログの bulk pull は行わない。
# ローカル .env には絶対に触らない（上書きも削除もしない）。
# push は deploy-to-vps.sh に委譲（.env 除外ルールを共有）。
set -euo pipefail

# ---- 固定パラメータ（環境変数で上書き可能） ----
VPS_IP="${VPS_IP:-203.0.113.10}"
VPS_USER="${VPS_USER:-deploy}"
REMOTE_DIR="${REMOTE_DIR:-/opt/cryptoengine}"
SSH_ALIAS="${SSH_ALIAS:-cryptoengine-vps}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SSH_OPTS="-o BatchMode=yes -o StrictHostKeyChecking=accept-new"

usage() {
  echo "usage: $(basename "$0") pull [--db] | push [--dry-run]" >&2
  exit 1
}

CMD="${1:-}"
[ -z "$CMD" ] && usage

case "$CMD" in
  pull)
    DATE="$(date +%Y%m%d)"
    DEST="$REPO_ROOT/ops/logs/remote/$DATE"
    mkdir -p "$DEST"
    echo "==> pull: 成果物のみ → $DEST"
    for f in trade_log.csv calibration_latest.json trader_heartbeat.json funding.jsonl vt_score_weekly.json; do
      rsync -avz \
        -e "ssh $SSH_OPTS" \
        "$SSH_ALIAS:$REMOTE_DIR/backend/core/trade_logs/$f" "$DEST/" || echo "skip: $f"
    done
    # V.T記録（小容量のJSONLのみ。板CSVのbulk回収はしない）
    for pat in "vt_signals_*.jsonl" "vt_shadow_*.jsonl"; do
      rsync -avz \
        -e "ssh $SSH_OPTS" \
        "$SSH_ALIAS:$REMOTE_DIR/backend/core/trade_logs/$pat" "$DEST/" || echo "skip: $pat"
    done
    if [ "${2:-}" = "--db" ]; then
      echo "==> pull: DB dump (read-only pg_dump)"
      ssh $SSH_OPTS "$SSH_ALIAS" \
        "cd '$REMOTE_DIR/infra/docker' && docker compose --env-file .env.prod -f docker-compose.yml -f docker-compose.prod.yml exec -T db pg_dump -U crypto -d crypto_engine --data-only" \
        > "$DEST/db-data-$DATE.sql"
      echo "saved: $DEST/db-data-$DATE.sql"
    fi
    echo "==> pull 完了: $DEST"
    ;;
  push)
    # deploy-to-vps.sh に委譲（--dry-run 対応・.env 除外共有）
    exec "$SCRIPT_DIR/deploy-to-vps.sh" "${2:-}"
    ;;
  *)
    usage
    ;;
esac
