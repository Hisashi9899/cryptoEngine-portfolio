#!/usr/bin/env bash
# Runs ON the VPS (via cron; see setup-vps.sh).
# Rotate time-series tables: delete rows older than RETENTION_DAYS.
#
# Env (set by the caller/cron): COMPOSE_DIR, COMPOSE_FILE
# Optional: DB_RETENTION_DAYS (default 90), DB_USER/DB_NAME (defaults below)
#
# 対象は時系列テーブルのみ。以下には絶対に触らない:
#   - positions / open_entries（ライブ状態。消すと稼働中ポジションを見失う）
#   - users / refresh_tokens（認証。消すと全員ログアウトする）
#   - trade_pairs の未クローズ行は存在しない設計だが、念のため exit_time=0 は除外
set -uo pipefail

: "${COMPOSE_DIR:?COMPOSE_DIR is required}"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.yml}"
RETENTION_DAYS="${DB_RETENTION_DAYS:-90}"
DB_USER="${DB_USER:-crypto}"
DB_NAME="${DB_NAME:-crypto_engine}"

# RETENTION_DAYS は整数のみ受理（SQL 埋め込み前のサニタイズ）。
case "$RETENTION_DAYS" in
  ''|*[!0-9]*) echo "rotate-db: ABORT: invalid DB_RETENTION_DAYS='$RETENTION_DAYS'" >&2; exit 1 ;;
esac

COMPOSE="docker compose -f $COMPOSE_DIR/$COMPOSE_FILE"

# NOTE: exec 系には必ず < /dev/null を付けること。
# 本スクリプトは ssh 経由で stdin から流し込まれる (bash -s) ため、
# stdin を開いたままのサブコマンドがあると残りのスクリプトを食い尽くす。
echo "==> rotate-db: retention=${RETENTION_DAYS}d tables=signals,trades,trade_pairs,balance_history,system_status"

# 1テーブルずつ削除し、件数をログに残す（監査用）。
# ms エポックのカットオフ計算は SQL 側で完結させ、シェル時刻への依存をなくす。
for spec in \
  "signals:timestamp:" \
  "trades:timestamp:" \
  "trade_pairs:exit_time:AND exit_time <> 0" \
  "balance_history:timestamp:" \
  "system_status:timestamp:"; do
  tbl="${spec%%:*}"
  rest="${spec#*:}"
  col="${rest%%:*}"
  extra="${rest#*:}"
  [ "$extra" = "$col" ] && extra=""
  # shellcheck disable=SC2086
  n=$($COMPOSE exec -T db psql -U "$DB_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1 -Atc \
    "WITH d AS (DELETE FROM $tbl WHERE $col < (EXTRACT(EPOCH FROM now() - make_interval(days => $RETENTION_DAYS))*1000)::BIGINT $extra RETURNING 1) SELECT count(*) FROM d;" \
    < /dev/null 2>&1) || {
    echo "rotate-db: FAILED on $tbl (aborting here; completed tables stay deleted)" >&2
    exit 1
  }
  echo "rotate-db: $tbl deleted_rows=$(echo "$n" | tr -d '[:space:]')"
done
echo "rotate-db: done (autovacuum reclaims space in background)"
