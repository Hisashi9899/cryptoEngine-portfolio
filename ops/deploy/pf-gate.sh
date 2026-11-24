#!/usr/bin/env bash
# PF連動の実弾/dry-run自動切替（VPS 上で実行・cron 駆動）。
#
#   bash /opt/cryptoengine/ops/deploy/pf-gate.sh [--apply]
#
# 既定は dry-run（判定のみ表示）。--apply で切替を実行する。
# 方針（安全側に非対称）：
#   - 不合格 → 即 dry-run 化（--live 削除＋ vtlive 再起動）
#   - 合格 → 2週連続合格でのみ実弾化（前回結果は vt_acceptance_prev.json）
#   - 判定不能（記録不足等）→ 現状維持
# 状態変化時は SMTP 設定があればメール通知、なければログのみ。
set -u

APP=/opt/cryptoengine
DC="/usr/bin/docker compose -f $APP/infra/docker/docker-compose.yml -f $APP/infra/docker/docker-compose.prod.yml"
LOGS=$APP/ops/logs/cron-pfgate.log
COMPOSE=$APP/infra/docker/docker-compose.yml
ACC=/trade_logs/vt_acceptance.json
PREV=$APP/backend/core/trade_logs/vt_acceptance_prev.json
CUR=$APP/backend/core/trade_logs/vt_acceptance.json
APPLY=0
if [ "${1:-}" = "--apply" ]; then
  APPLY=1
fi

log() { echo "$(date -u +%FT%TZ) pf-gate: $*" | tee -a "$LOGS"; }

run_accept() {
  # 判定は実勢Taker（片道0.05%）で行う。従来の 0.3% 往復固定は実勢の
  # 2〜3倍保守的で、合格可能な戦略を不合格にしていた（⑥）。
  $DC exec -T core core vt-acceptance --dir=/trade_logs --days=14 --bars=300 \
    --fee-mode=taker --min-pairs=200 --min-pf=1.2 --out=$ACC >>"$LOGS" 2>&1
  # Maker リベート前提の参考値（判定には使わない。実勢確認用）。
  $DC exec -T core core vt-acceptance --dir=/trade_logs --days=14 --bars=300 \
    --fee-mode=maker --min-pairs=200 --min-pf=1.2 \
    --out=/trade_logs/vt_acceptance_maker.json >>"$LOGS" 2>&1
}

pass_of() {
  python3 -c "import json;print(json.load(open('$1')).get('pass',False))" 2>/dev/null
}

live_on() { grep -q '^      --live$' "$COMPOSE"; }

if [ "$APPLY" = "1" ]; then
  run_accept
else
  log "dry-run check only (no changes)"
fi

PASS="$(pass_of "$CUR")"
PREVPASS="False"
if [ -f "$PREV" ]; then
  PREVPASS="$(pass_of "$PREV")"
fi
log "accept pass=$PASS prev=$PREVPASS"
if [ "$APPLY" = "1" ]; then
  cp -f "$CUR" "$PREV"
fi

want="keep"
if [ "$PASS" != "True" ]; then
  want="dryrun"
elif [ "$PREVPASS" = "True" ]; then
  want="live"
else
  want="keep"
fi

if live_on; then cur="live"; else cur="dryrun"; fi
log "want=$want current=$cur"
if [ "$APPLY" != "1" ] || [ "$want" = "keep" ] || [ "$want" = "$cur" ]; then
  log "no change"
  exit 0
fi

if [ "$want" = "dryrun" ]; then
  sed -i '/^      --live$/d' "$COMPOSE"
  log "ACTION switch to dryrun"
else
  sed -i '/^      --margin$/i\      --live' "$COMPOSE"
  log "ACTION switch to live (2-week streak)"
fi
cd "$APP" || exit 1
/usr/bin/docker compose -f infra/docker/docker-compose.yml up -d vtlive >>"$LOGS" 2>&1
log "vtlive restarted"
