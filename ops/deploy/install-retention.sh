#!/usr/bin/env bash
# VPS-contained retention installer (VPS 上で実行・冪等).
#   bash ops/deploy/install-retention.sh
# やること:
#   1) DB ローテを 90d → 45d（短期窓ポリシー）
#   2) prune-trade-logs.sh の日次 cron を追加（03:40 JST）
#   3) GMO 板収集を 5 → 17 銘柄へ拡張（残り12は --interval=5 で軽量化）
set -euo pipefail

APP_DIR="/opt/cryptoengine"
DC="docker compose -f $APP_DIR/infra/docker/docker-compose.yml -f $APP_DIR/infra/docker/docker-compose.prod.yml"
CAPLOG="$APP_DIR/ops/logs/cron-cap-gmo.log"
BASE="$DC exec -T core core capture-daily --exchange=gmo"

TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT
crontab -l 2>/dev/null > "$TMP" || true

echo "==> [1/3] DB retention 90d -> 45d"
sed -i 's/DB_RETENTION_DAYS=90/DB_RETENTION_DAYS=45/' "$TMP"

echo "==> [2/3] prune cron (daily 03:40 JST)"
if ! grep -q 'prune-trade-logs.sh' "$TMP"; then
  cat >> "$TMP" <<'EOF'
40 3 * * * /usr/bin/flock -n /tmp/crypto-prune.lock bash /opt/cryptoengine/ops/scripts/remote/prune-trade-logs.sh >> /opt/cryptoengine/ops/logs/cron-prune.log 2>&1
EOF
  echo "added prune line"
else
  echo "prune line already present (skip)"
fi

echo "==> [3/3] GMO capture 5 -> 17 symbols"
ADDED=0
# sleep,lock,symbol (既存5銘柄とlock重複なし・10分枠内に分散)
for spec in \
  "30:crypto-cap-ada:ADA" \
  "75:crypto-cap-astr:ASTR" \
  "150:crypto-cap-atom:ATOM" \
  "195:crypto-cap-bch:BCH" \
  "270:crypto-cap-dot:DOT" \
  "315:crypto-cap-fcr:FCR" \
  "390:crypto-cap-link:LINK" \
  "435:crypto-cap-ltc:LTC" \
  "480:crypto-cap-nac:NAC" \
  "525:crypto-cap-sui:SUI" \
  "570:crypto-cap-wild:WILD" \
  "45:crypto-cap-xlm:XLM"; do
  sleep_s="${spec%%:*}"
  rest="${spec#*:}"
  lock="${rest%%:*}"
  sym="${rest##*:}"
  if grep -q "$lock" "$TMP"; then
    continue
  fi
  echo "*/10 * * * * sleep $sleep_s; /usr/bin/flock -n /tmp/$lock.lock $BASE --symbol=$sym --seconds=420 --interval=5 >> $CAPLOG 2>&1" >> "$TMP"
  ADDED=$((ADDED + 1))
done
echo "added $ADDED capture lines"

crontab "$TMP"
echo "==> current cryptoEngine cron lines:"
crontab -l 2>/dev/null | grep -c 'crypto' | xargs -I{} echo "crypto lines: {}"
crontab -l 2>/dev/null | grep -E 'prune|RETENTION_DAYS' | head -n 5
