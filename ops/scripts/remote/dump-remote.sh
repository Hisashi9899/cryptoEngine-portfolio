#!/usr/bin/env bash
# Runs ON the VPS via: ssh ... 'STAGING_DIR=... COMPOSE_DIR=... bash -s' < dump-remote.sh
# Pure collection — NO analysis logic lives on the VPS.
# Env (set by the caller): STAGING_DIR, COMPOSE_DIR, COMPOSE_FILE, NGINX_ACCESS_LOG, NGINX_ERROR_LOG, DATE
set -uo pipefail

: "${STAGING_DIR:?STAGING_DIR is required}"
: "${COMPOSE_DIR:?COMPOSE_DIR is required}"
: "${DATE:?DATE is required}"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.yml}"

OUT="$STAGING_DIR/$DATE"
mkdir -p "$OUT/docker" "$OUT/nginx" "$OUT/system" "$OUT/db" "$OUT/market" "$OUT/trading"

COMPOSE="docker compose -f $COMPOSE_DIR/$COMPOSE_FILE"
# 実データの取得元（VPS 側パス。collect.sh から上書き可能）。
# ORDERBOOK_LOG_DIR: orderbook_YYYYMMDD.csv / signals_v2_*.jsonl / calibration_*.json
# COMPOSE_DIR のレイアウト差異（repo直下 / infra/docker）に頑健なよう候補探索する。
if [ -z "${ORDERBOOK_LOG_DIR:-}" ]; then
  ORDERBOOK_LOG_DIR=""
  for cand in "$COMPOSE_DIR/backend/core/trade_logs" \
              "$COMPOSE_DIR/../backend/core/trade_logs" \
              "$COMPOSE_DIR/../../backend/core/trade_logs"; do
    if [ -d "$cand" ]; then ORDERBOOK_LOG_DIR="$cand"; break; fi
  done
  : "${ORDERBOOK_LOG_DIR:=$COMPOSE_DIR/backend/core/trade_logs}"
fi
TRADE_LOG_CSV="${TRADE_LOG_CSV:-$ORDERBOOK_LOG_DIR/trade_log.csv}"

echo "==> dumping docker logs (last 24h)"
$COMPOSE logs --since 24h api        > "$OUT/docker/api.log" 2>&1 || true
$COMPOSE logs --since 24h core       > "$OUT/docker/core.log" 2>&1 || true
$COMPOSE logs --since 24h frontend   > "$OUT/docker/frontend.log" 2>&1 || true
$COMPOSE logs --since 24h db         > "$OUT/docker/db.log" 2>&1 || true

echo "==> dumping nginx logs"
if [ -n "${NGINX_ACCESS_LOG:-}" ] && [ -r "$NGINX_ACCESS_LOG" ]; then
  cp "$NGINX_ACCESS_LOG" "$OUT/nginx/access.log" 2>/dev/null || true
fi
if [ -n "${NGINX_ERROR_LOG:-}" ] && [ -r "$NGINX_ERROR_LOG" ]; then
  cp "$NGINX_ERROR_LOG" "$OUT/nginx/error.log" 2>/dev/null || true
fi

echo "==> dumping system metrics"
{
  echo "### uptime"; uptime
  echo "### free -m"; free -m
  echo "### df -h"; df -h / | tail -1
  echo "### docker ps"; docker ps --format '{{.Names}}\t{{.Status}}' 2>/dev/null || true
  echo "### docker stats"; timeout 10 docker stats --no-stream 2>/dev/null || true
} > "$OUT/system/metrics.txt"

# NOTE: exec 系には必ず < /dev/null を付けること。
# 本スクリプトは ssh 経由で stdin から流し込まれる (bash -s) ため、
# stdin を開いたままのサブコマンドがあると残りのスクリプトを食い尽くし、
# bash が EOF で途中終了する（market/trading/db ダンプが消える）。
echo "==> dumping db status"
{
  echo "### pg_isready"; $COMPOSE exec -T db pg_isready -U crypto -d crypto_engine 2>&1 < /dev/null || true
  echo "### pg_stat_activity"
  $COMPOSE exec -T db psql -U crypto -d crypto_engine -c "SELECT count(*) AS connections, state FROM pg_stat_activity GROUP BY state;" 2>&1 < /dev/null || true
} > "$OUT/db/postgres.txt"

echo "==> dumping market data (orderbook daily CSVs)"
# 永続化された orderbook_YYYYMMDD.csv を取得。無ければ空のまま（後段で除外）。
cp "$ORDERBOOK_LOG_DIR"/orderbook_*.csv "$OUT/market/" 2>/dev/null || true
ls "$OUT/market/" 2>/dev/null || echo "(no market CSVs)"

echo "==> dumping trading data (trade log, paper signals, calibration)"
if [ -r "$TRADE_LOG_CSV" ]; then
  cp "$TRADE_LOG_CSV" "$OUT/trading/trade_log.csv" 2>/dev/null || true
else
  echo "(no trade_log.csv)" >&2
fi
cp "$ORDERBOOK_LOG_DIR"/signals_v2_*.jsonl "$OUT/trading/" 2>/dev/null || true
cp "$ORDERBOOK_LOG_DIR"/calibration_*.json "$OUT/trading/" 2>/dev/null || true
ls "$OUT/trading/" 2>/dev/null || echo "(no trading files)"

echo "==> dumping table data (signals / trades / trade_pairs, data-only)"
$COMPOSE exec -T db pg_dump -U crypto -d crypto_engine \
  -t signals -t trades -t trade_pairs --data-only --column-inserts \
  < /dev/null > "$OUT/db/tables.sql" 2>/dev/null || echo "(pg_dump unavailable)" > "$OUT/db/tables.sql"

echo "==> writing manifest"
shopt -s nullglob
(cd "$OUT" && sha256sum market/* trading/* db/tables.sql 2>/dev/null > MANIFEST.sha256) || true
shopt -u nullglob

echo "==> writing vars.env (resolved paths for later prune steps)"
# collect.sh の prune 処理が VPS 側の実パスを知るための記録。
# 中身はパスのみ（秘密情報を含めない）。
printf 'ORDERBOOK_LOG_DIR=%s\n' "$ORDERBOOK_LOG_DIR" > "$OUT/vars.env"

echo "==> done: $OUT"
ls -la "$OUT"
