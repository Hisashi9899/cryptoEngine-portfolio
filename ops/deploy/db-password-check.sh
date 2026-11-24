#!/usr/bin/env bash
# DB パスワード整合の読み取り専用診断（VPS 上で実行）。
# 秘密値は絶対に表示しない。MATCH/MISMATCH と疎通可否のみ出力する。
#
#   bash ops/deploy/db-password-check.sh   (infra/docker で実行すること)
set -uo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../../infra/docker"

pw_of_url() {
  # DB_URL からパスワード部だけ抜く（表示しない）
  printf '%s' "$1" | sed -E 's#.*://[^:]+:([^@]+)@.*#\1#'
}

DOCKER_ENV=".env"
CONFIG_ENV="../config/.env"

PG_DOCKER=$(grep '^POSTGRES_PASSWORD=' "$DOCKER_ENV" 2>/dev/null | head -1 | cut -d= -f2-)
URL_DOCKER=$(grep '^DB_URL=' "$DOCKER_ENV" 2>/dev/null | head -1 | cut -d= -f2-)
URL_CONFIG=$(grep '^DB_URL=' "$CONFIG_ENV" 2>/dev/null | head -1 | cut -d= -f2-)

echo "== config.env DB_URL host (must be db for containers)"
printf '%s' "${URL_CONFIG:-}" | sed -E 's#.*://[^@]+@([^/:]+).*#\1#'
echo "(above must print: db)"
echo ""
echo "== presence"
[ -n "${PG_DOCKER:-}" ] && echo "docker.env POSTGRES_PASSWORD: set" || echo "docker.env POSTGRES_PASSWORD: empty"
[ -n "${URL_DOCKER:-}" ] && echo "docker.env DB_URL: set" || echo "docker.env DB_URL: default(demo_local_password)"
[ -n "${URL_CONFIG:-}" ] && echo "config.env DB_URL: set" || echo "config.env DB_URL: empty"

echo "== static match (compose effective vs db init)"
# core の environment: は docker.env DB_URL があればそれ、無ければ既定 demo_local_password が有効値
if [ -n "${URL_DOCKER:-}" ]; then
  EFFECTIVE_PW=$(pw_of_url "$URL_DOCKER")
  echo "effective source: docker.env DB_URL"
else
  EFFECTIVE_PW="demo_local_password"
  echo "effective source: compose default"
fi
if [ -n "${PG_DOCKER:-}" ] && [ "$PG_DOCKER" = "$EFFECTIVE_PW" ]; then
  echo "POSTGRES_PASSWORD vs effective DB_URL: MATCH"
else
  echo "POSTGRES_PASSWORD vs effective DB_URL: MISMATCH"
fi
if [ -n "${URL_CONFIG:-}" ]; then
  if [ "$(pw_of_url "$URL_CONFIG")" = "$EFFECTIVE_PW" ]; then
    echo "config.env DB_URL vs effective: MATCH"
  else
    echo "config.env DB_URL vs effective: MISMATCH (env_fileよりenvironment:が優先されるため実害なし)"
  fi
fi

echo "== live probe (which password does db accept?)"
probe() {
  local label="$1" pw="$2"
  [ -z "$pw" ] && { echo "$label: skip(empty)"; return; }
  # -h 127.0.0.1 で TCP 接続すること（Unix socket は trust で認証素通りのため）。
  if docker compose exec -T db env "PGPASSWORD=$pw" psql -h 127.0.0.1 -U crypto -d crypto_engine -tc 'SELECT 1' 2>/dev/null | grep -q 1; then
    echo "$label: ACCEPTED"
  else
    echo "$label: rejected"
  fi
}
probe "live TCP-self vs POSTGRES_PASSWORD(docker.env)" "$PG_DOCKER"
probe "live TCP-self vs effective-DB_URL-password" "$EFFECTIVE_PW"
probe "live TCP-self vs config.env-DB_URL-password" "$(pw_of_url "${URL_CONFIG:-}")"
echo "(TCP-self は 127.0.0.1 trust のため参考値。真の判定は remote が正)"

echo "== live probe remote (db イメージの一時コンテナから compose network 経由で TCP 接続)"
rprobe() {
  local label="$1" pw="$2"
  [ -z "$pw" ] && { echo "$label: skip(empty)"; return; }
  if docker compose run --rm db env "PGPASSWORD=$pw" psql -h db -U crypto -d crypto_engine -tc 'SELECT 1' 2>/dev/null | grep -q 1; then
    echo "$label: ACCEPTED"
  else
    echo "$label: rejected"
  fi
}
rprobe "live TCP-remote vs config.env-DB_URL-password" "$(pw_of_url "${URL_CONFIG:-}")"
rprobe "live TCP-remote vs demo_local_password" "demo_local_password"

echo "== resolved config (hashes only, never values)"
if [ -n "${DB_URL:-}" ]; then
  echo "shell-exported DB_URL: present (overrides .env files in interpolation!)"
else
  echo "shell-exported DB_URL: absent"
fi
h() { printf '%s' "$1" | sha256sum | cut -d' ' -f1 | cut -c1-16; }
RESOLVED_URL=$(docker compose config 2>/dev/null | grep -i 'DB_URL' | head -1 | sed -E 's/^[^:]*:\s*//')
echo "resolved core DB_URL line present: $([ -n "$RESOLVED_URL" ] && echo yes || echo no)"
echo "hash(resolved core pw)   = $(h "$(pw_of_url "$RESOLVED_URL")")"
echo "hash(demo_local_password)         = $(h demo_local_password)"
echo "hash(docker.env POSTGRES)= $(h "${PG_DOCKER:-}")"
echo "hash(config.env DB_URL)  = $(h "$(pw_of_url "${URL_CONFIG:-}")")"
echo "(equal hashes = same password; values never shown)"
