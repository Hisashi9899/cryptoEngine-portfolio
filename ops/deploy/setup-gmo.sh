#!/usr/bin/env bash
# GMO読み取り専用キーのVPS安全設定（VPS上で実行）
#   bash ops/deploy/setup-gmo.sh
# - 秘密鍵はVPS上のみに保存し、rsync転送対象外（.env.prodは除外リスト済み）
# - 読み取り専用キー推奨（取引権限はauto-loop用に別途検討）
# - 設定後にapi/coreへEXCHANGE_ID=gmoを波及させ再起動＋疎通確認する
set -euo pipefail

APP_DIR="/opt/cryptoengine"
ENV_PROD="$APP_DIR/infra/config/.env.prod"
COMPOSE_DIR="$APP_DIR/infra/docker"

if [ ! -f "$ENV_PROD" ]; then
  echo "ABORT: $ENV_PROD がありません。先に setup-vps.sh を実行してください" >&2
  exit 1
fi

echo "==> 現在のGMO設定（長さのみ表示）"
for k in EXCHANGE_ID GMO_API_KEY GMO_API_SECRET; do
  v="$(grep -E "^${k}=" "$ENV_PROD" | cut -d= -f2- || true)"
  echo "$k len=${#v}"
done

echo ""
echo "GMOコインのAPIキー（読み取り専用推奨）を入力してください。空Enterでスキップします。"
read -rsp "GMO_API_KEY: " IN_KEY; echo
read -rsp "GMO_API_SECRET: " IN_SEC; echo

if [ -n "${IN_KEY:-}" ]; then
  # 特殊文字を安全に置換するためpythonで更新する
  IN_KEY="$IN_KEY" IN_SEC="$IN_SEC" ENV_PROD="$ENV_PROD" python3 - <<'PY'
import os
p = os.environ["ENV_PROD"]
k = os.environ.get("IN_KEY", "")
s = os.environ.get("IN_SEC", "")
txt = open(p, encoding="utf-8").read().splitlines(True)
def upsert(lines, key, val):
    found = False
    out = []
    for line in lines:
        if line.startswith(key + "="):
            out.append(f"{key}={val}\n")
            found = True
        else:
            out.append(line)
    if not found:
        if out and not out[-1].endswith("\n"):
            out[-1] += "\n"
        out.append(f"{key}={val}\n")
    return out
txt = upsert(txt, "GMO_API_KEY", k)
txt = upsert(txt, "GMO_API_SECRET", s)
open(p, "w", encoding="utf-8").writelines(txt)
PY
  chmod 600 "$ENV_PROD"
  echo "GMO_API_KEY/SECRETを更新しました（VPS上のみ）"
fi

# EXCHANGE_ID=gmoへ切替（GMO執行・余力判定を有効化）
if grep -qE '^EXCHANGE_ID=' "$ENV_PROD"; then
  sed -i 's/^EXCHANGE_ID=.*/EXCHANGE_ID=gmo/' "$ENV_PROD"
else
  echo "EXCHANGE_ID=gmo" >> "$ENV_PROD"
fi
echo "EXCHANGE_ID=gmo に設定しました"

# docker interpolation用 .env.prod を再同期（setup-vps.shと同一リスト）
DOCKER_ENV="$COMPOSE_DIR/.env.prod"
: > "$DOCKER_ENV"
chmod 600 "$DOCKER_ENV"
for k in AUTH_SECRET JWT_ACCESS_TTL_HOURS JWT_REFRESH_TTL_DAYS \
         AUTH_ADMIN_USERNAME AUTH_ADMIN_EMAIL AUTH_ADMIN_PASSWORD \
         EXCHANGE_BASE_URL EXCHANGE_TICKER_PATH APP_ENV EXCHANGE_ID \
         GMO_API_KEY GMO_API_SECRET BINANCE_API_KEY BINANCE_API_SECRET \
         AUTO_TRADE_ENABLED MAX_POSITION_SIZE_USDT MAX_DAILY_LOSS_USDT STOP_ON_LOSS TRADE_SIZE_USDT \
         DB_URL DB_HOST DB_PORT DB_USER DB_PASSWORD DB_NAME \
         POSTGRES_USER POSTGRES_PASSWORD POSTGRES_DB; do
  grep -E "^${k}=" "$ENV_PROD" >> "$DOCKER_ENV" || true
done
echo "synced: $DOCKER_ENV"

echo "==> api/core/vtlive再起動（vtliveはgmo環境を引き継ぐため再作成）"
cd "$COMPOSE_DIR"
DC="docker compose --env-file .env.prod -f docker-compose.yml -f docker-compose.prod.yml"
$DC up -d api core
# vtliveはtradingプロファイルのため、起動中の場合のみ再作成する。
# GMOキーは起動時読込のため、変更時は再作成が必須。停止中は触らない。
if $DC ps 2>/dev/null | grep -q "vtlive.*Up"; then
  $DC --profile trading up -d --force-recreate vtlive 2>/dev/null || true
fi
sleep 8
$DC ps

echo "==> 疎通確認（キー未設定ならGMO_API_KEY未設定と表示されます）"
ADMIN_USER="$(grep -E '^AUTH_ADMIN_USERNAME=' "$ENV_PROD" | cut -d= -f2-)"
ADMIN_PASS="$(grep -E '^AUTH_ADMIN_PASSWORD=' "$ENV_PROD" | cut -d= -f2-)"
TOKEN="$(curl -s -X POST http://localhost:8000/api/v1/auth/login -H 'Content-Type: application/json' -d "{\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASS\"}" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("access_token",""))')"
if [ -z "$TOKEN" ]; then
  echo "ABORT: login失敗" >&2
  exit 1
fi
echo "--- /api/v1/gmo/balance ---"
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8000/api/v1/gmo/balance | head -c 800; echo
echo "--- /api/v1/auto-trade/affordability ---"
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8000/api/v1/auto-trade/affordability | head -c 800; echo
echo "==> setup-gmo.sh 完了。ダッシュボード System/Overview/Balance のGMO実残高・取引可否を確認してください"
