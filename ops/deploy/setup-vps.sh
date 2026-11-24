#!/usr/bin/env bash
# cryptoEngine VPS 初期セットアップ（VPS 上で実行）
#
#   実行場所: /opt/cryptoengine（デプロイ済み前提）
#   実行方法: bash ops/deploy/setup-vps.sh
#
# やること:
#   1) docker / compose plugin の確認（なければ公式手順でインストール）
#   2) docker グループ付与（初回は再ログイン後に再実行）
#   3) infra/config/.env.prod を .env.example から生成（存在すれば絶対に上書きしない）
#   4) ログディレクトリ作成
#   5) 本番 compose 起動（--env-file .env.prod + prod override）
#   5b) DB 接続修復・管理者同期検証（古い pgdata 対策。データ消去なし）
#   6) ヘルスチェック
#   7) cron 登録（capture-daily / paper）
#
# APIキー（BINANCE_API_KEY/SECRET）は空欄のままjala起動する。
# 取引所連携が必要になったら .env.prod を直接編集すること（転送厳禁）。
set -euo pipefail

# ---- 固定パラメータ（環境変数で上書き可能） ----
APP_DIR="${APP_DIR:-/opt/cryptoengine}"
COMPOSE_DIR="$APP_DIR/infra/docker"
ENV_PROD="$APP_DIR/infra/config/.env.prod"
ENV_EXAMPLE="$APP_DIR/infra/config/.env.example"

echo "==> [1/7] docker / compose 確認"
if ! command -v docker >/dev/null 2>&1; then
  echo "docker がないため公式スクリプトでインストールします（sudo が必要）"
  curl -fsSL https://get.docker.com | sudo sh
fi
if ! docker compose version >/dev/null 2>&1; then
  echo "ABORT: docker compose plugin が使えません" >&2
  exit 1
fi
docker --version
docker compose version

echo "==> [2/7] docker グループ確認"
if ! docker ps >/dev/null 2>&1; then
  echo "docker グループに $USER を追加します"
  sudo usermod -aG docker "$USER"
  echo "初回のため再ログイン後に本スクリプトを再実行してください: bash $0"
  echo "（以降の手順は続行しますが docker 操作は次回以降有効になります）"
  DOCKER_OK=0
else
  echo "docker 操作 OK"
  DOCKER_OK=1
fi

echo "==> [3/7] .env.prod 生成（存在すれば絶対に上書きしない）"
if [ -f "$ENV_PROD" ]; then
  echo "exists (keep, no overwrite): $ENV_PROD"
else
  cp "$ENV_EXAMPLE" "$ENV_PROD"
  chmod 600 "$ENV_PROD"
  echo "generated from .env.example (APIキーは空欄のまま): $ENV_PROD"
fi
chmod 600 "$ENV_PROD" 2>/dev/null || true
# 未設定の秘密値を自動発行（空欄放置の事故防止。値は VPS 上のみに保存）
# - AUTH_SECRET: 空なら hex 32byte
# - DB 系: 空なら共通パスワードを発行し DB_URL / DB_PASSWORD / POSTGRES_PASSWORD
#   の3箇所に同期設定（compose ネットワーク内のホスト名は db を使用）
# - AUTH_ADMIN_PASSWORD: 空なら hex 12byte
# ※ BINANCE_API_KEY/SECRET は空欄のまま（取引所連携時に手動設定）
if grep -qE '^AUTH_SECRET=$' "$ENV_PROD"; then
  sed -i "s/^AUTH_SECRET=$/AUTH_SECRET=$(openssl rand -hex 32)/" "$ENV_PROD"
  echo "AUTH_SECRET を自動発行しました"
fi
if grep -qE '^DB_PASSWORD=$' "$ENV_PROD"; then
  DBPW="$(openssl rand -hex 16)"
  sed -i "s/^DB_PASSWORD=$/DB_PASSWORD=$DBPW/" "$ENV_PROD"
  sed -i "s/^POSTGRES_PASSWORD=$/POSTGRES_PASSWORD=$DBPW/" "$ENV_PROD"
  # DB_URL が空なら compose 用に組み立てる（既存値がある場合は触らない）
  if grep -qE '^DB_URL=$' "$ENV_PROD"; then
    sed -i "s|^DB_URL=$|DB_URL=postgres://crypto:${DBPW}@db:5432/crypto_engine|" "$ENV_PROD"
  fi
  sed -i "s/^DB_HOST=.*/DB_HOST=db/" "$ENV_PROD"
  echo "DB パスワードを自動発行し DB_URL / POSTGRES_PASSWORD に同期しました"
fi
if grep -qE '^AUTH_ADMIN_PASSWORD=$' "$ENV_PROD"; then
  PW="$(openssl rand -hex 12)"
  sed -i "s/^AUTH_ADMIN_PASSWORD=$/AUTH_ADMIN_PASSWORD=$PW/" "$ENV_PROD"
  echo "AUTH_ADMIN_PASSWORD を自動発行しました（$ENV_PROD に記録）"
fi
# compose 管理下では DB ホスト名は必ず db（localhost ではコンテナ間接続不可）
sed -i "s/^DB_HOST=.*/DB_HOST=db/" "$ENV_PROD"
# 本番ファイルのため APP_ENV は prod に固定（起動時検証を有効化）
sed -i "s/^APP_ENV=.*/APP_ENV=prod/" "$ENV_PROD"

echo "==> [3b/7] compose interpolation 用 .env.prod を同期"
# infra/docker/.env.prod は compose の ${VAR} 解決専用の派生ファイル。
# config/.env.prod を単一真実源とし、毎回再生成する（古い値の残留による
# interpolation stale を防ぐ。独立した状態は持たないため上書きは安全）。
DOCKER_ENV="$COMPOSE_DIR/.env.prod"
if [ -f "$DOCKER_ENV" ]; then
  cp -p "$DOCKER_ENV" "$DOCKER_ENV.bak"
  echo "backup: $DOCKER_ENV.bak"
fi
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
echo "synced: $DOCKER_ENV (from $ENV_PROD)"

echo "==> [4/7] ログディレクトリ作成"
mkdir -p "$APP_DIR/backend/core/trade_logs"
mkdir -p "$APP_DIR/ops/logs"
chmod 755 "$APP_DIR/ops/logs"

if [ "$DOCKER_OK" != "1" ]; then
  echo "==> docker 権限待ちのため [5/7] 以降は再ログイン後に再実行してください"
  exit 0
fi

echo "==> [5/7] 本番 compose 起動"
cd "$COMPOSE_DIR"
DC="docker compose --env-file .env.prod -f docker-compose.yml -f docker-compose.prod.yml"
$DC up -d --build
# vtlive（実弾執行）は profile: trading のため通常の up では起動しない。
# 2026-09-26 に trader サービスは削除済みのため、ここでは自動起動しない。
# 実弾を開始するときは運用者が明示的に行うこと:
#   $DC --profile trading up -d vtlive
$DC ps

echo "==> [5b/7] DB 接続修復・管理者同期検証（古い pgdata 対策）"
# POSTGRES_PASSWORD は初回 volume 作成時のみ有効。古い pgdata が残っていると
# api の新パスワードで認証失敗し、ログイン不可になる。ここで検出・修復する。
# （データ消去はしない。volume を捨てる操作は一切含まない）
DB_PW="$(grep -E '^POSTGRES_PASSWORD=' "$ENV_PROD" | cut -d= -f2-)"
DB_USER="$(grep -E '^POSTGRES_USER=' "$ENV_PROD" | cut -d= -f2-)"
DB_NAME="$(grep -E '^POSTGRES_DB=' "$ENV_PROD" | cut -d= -f2-)"
: "${DB_USER:=crypto}"
: "${DB_NAME:=crypto_engine}"
if [ -z "$DB_PW" ]; then
  echo "ABORT: POSTGRES_PASSWORD が空です。.env.prod を確認してください" >&2
  exit 1
fi
echo "db 起動待ち..."
for i in $(seq 1 30); do
  if $DC exec -T db pg_isready -U "$DB_USER" -d "$DB_NAME" >/dev/null 2>&1; then
    break
  fi
  if [ "$i" = "30" ]; then
    echo "ABORT: db が起動しません。$DC logs db を確認してください" >&2
    exit 1
  fi
  sleep 5
done
# api と同一条件（TCP＋パスワード）で接続試験
if $DC exec -T -e PGPASSWORD="$DB_PW" db psql -h localhost -U "$DB_USER" -d "$DB_NAME" -c "SELECT 1" >/dev/null 2>&1; then
  echo "DB auth OK（api と同一条件で接続成功）"
else
  echo "password mismatch を検出。DB 内パスワードを .env.prod に合わせて修復します"
  PW_ESC=${DB_PW//\'/\'\'}
  $DC exec -T db psql -U "$DB_USER" -d "$DB_NAME" -c "ALTER USER \"$DB_USER\" WITH PASSWORD '$PW_ESC';"
  echo "api を再起動して init_db＋管理者同期を再実行します"
  $DC restart api
fi
# users 件数で管理者同期を検証（最大60秒待機）
SYNCED=0
for i in $(seq 1 12); do
  USERS="$($DC exec -T db psql -U "$DB_USER" -d "$DB_NAME" -Atc "SELECT count(*) FROM users;" 2>/dev/null | tr -d '[:space:]' || echo 0)"
  if [ -n "$USERS" ] && [ "$USERS" != "0" ]; then
    SYNCED=1
    break
  fi
  sleep 5
done
if [ "$SYNCED" != "1" ]; then
  echo "ABORT: users テーブルに管理者がいません。api ログを確認してください" >&2
  $DC logs api 2>&1 | grep -i -E "database unavailable|synced admin|password authentication" | tail -5 || true
  exit 1
fi
echo "users count: $USERS（管理者同期 OK）"
$DC logs api 2>&1 | grep -i "synced admin" | tail -2 || true

echo "==> [6/7] ヘルスチェック"
for i in $(seq 1 30); do
  if curl -sf http://localhost:8000/health >/dev/null 2>&1; then
    echo "API healthy"
    break
  fi
  if [ "$i" = "30" ]; then
    echo "ABORT: API が起動しません。docker compose logs api を確認してください" >&2
    exit 1
  fi
  sleep 5
done

echo "==> [6b/7] login smoke test（ヘルスチェック通過後に実施）"
# DB 経由認証パスの smoke test：誤パスワードで 401 が返れば DB 参照が動作している
# （500 が返る場合は DB 未到達。429 の場合はレート制限中のため警告のみ）
ADMIN_USER="$(grep -E '^AUTH_ADMIN_USERNAME=' "$ENV_PROD" | cut -d= -f2-)"
: "${ADMIN_USER:=admin}"
CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8000/api/v1/auth/login -H 'Content-Type: application/json' -d "{\"username\":\"$ADMIN_USER\",\"password\":\"__invalid__\"}" || true)
[ -z "$CODE" ] && CODE="no-response"
if [ "$CODE" = "401" ]; then
  echo "login path OK（誤パスワードで 401＝DB 参照が動作）"
else
  echo "WARNING: login smoke test returned $CODE (expected 401)" >&2
fi

echo "==> [7/7] cron 登録（capture-daily / vt-paper）"
if ! command -v crontab >/dev/null 2>&1; then
  echo "cron 未導入のためインストールします"
  sudo apt-get update -qq && sudo apt-get install -y -qq cron
  sudo systemctl enable --now cron 2>/dev/null || sudo service cron start 2>/dev/null || true
fi
command -v flock >/dev/null 2>&1 || sudo apt-get install -y -qq util-linux
CRON_TMP="$(mktemp)"
crontab -l 2>/dev/null | grep -v "cryptoEngine" > "$CRON_TMP" || true
cat >> "$CRON_TMP" <<EOF
# cryptoEngine: orderbook 日次キャプチャ（10分毎・重複防止）
*/10 * * * * /usr/bin/flock -n /tmp/crypto-capture.lock /usr/bin/docker compose -f $COMPOSE_DIR/docker-compose.yml -f $COMPOSE_DIR/docker-compose.prod.yml exec -T core core capture-daily --seconds=540 >> $APP_DIR/ops/logs/cron-capture.log 2>&1
# cryptoEngine: V.T paper 記録（10分毎・420秒 bounded・重複防止、注文なし）
# 注意: docker exec は ENTRYPOINT を無視するためバイナリ名 core を明示する
# （v1/v2/v3 退役済み。v2 paper は retire-v2-cron.sh で無効化済み）
*/10 * * * * /usr/bin/flock -n /tmp/crypto-vt-setup.lock /usr/bin/docker compose -f $COMPOSE_DIR/docker-compose.yml -f $COMPOSE_DIR/docker-compose.prod.yml exec -T core core vt-paper --symbol=BTC --bars=300 --seconds=420 >> $APP_DIR/ops/logs/cron-vt.log 2>&1
# cryptoEngine: DB ローテーション（毎日 03:17 JST、時系列テーブル45日保持）
# VPS 完結の短期窓ポリシー（ops/PRODUCT_DATA_POLICY.md）。
# positions / open_entries（ライブ状態）と users / refresh_tokens（認証）には触らない。
# 保持期間の変更は DB_RETENTION_DAYS で上書き可能。
17 3 * * * /usr/bin/flock -n /tmp/crypto-rotate-db.lock env COMPOSE_DIR=$COMPOSE_DIR COMPOSE_FILE=docker-compose.yml DB_RETENTION_DAYS=45 bash $APP_DIR/ops/scripts/remote/rotate-db.sh >> $APP_DIR/ops/logs/cron-rotate-db.log 2>&1
# cryptoEngine: 生データの保持上限（毎日 03:40 JST、板14日・signals14日・cronログ上限）
# trade_log.csv / trader_heartbeat.json / calibration_latest.json には触らない。
40 3 * * * /usr/bin/flock -n /tmp/crypto-prune.lock bash $APP_DIR/ops/scripts/remote/prune-trade-logs.sh >> $APP_DIR/ops/logs/cron-prune.log 2>&1
EOF
crontab "$CRON_TMP"
rm -f "$CRON_TMP"
crontab -l | grep cryptoEngine

echo "==> setup-vps.sh 完了"
echo "確認: https://<VPS-IP>:3001 (frontend) / http://localhost:8000/health (VPS内)"
