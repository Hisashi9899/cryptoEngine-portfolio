#!/usr/bin/env bash
# docker.env の POSTGRES_PASSWORD を config.env の DB_URL と一致させる。
# （将来の volume 再作成時に live DB と一致させるための予防。実行中 DB には触らない）
# 秘密値は表示しない。VPS 上で実行すること。
#
#   bash ops/deploy/db-align-env.sh   (infra/docker で実行すること)
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../../infra/docker"

URL_CONFIG=$(grep '^DB_URL=' ../config/.env 2>/dev/null | head -1 | cut -d= -f2-)
if [ -z "${URL_CONFIG:-}" ]; then
  echo "ABORT: ../config/.env に DB_URL がありません" >&2
  exit 1
fi
PW=$(printf '%s' "$URL_CONFIG" | sed -E 's#.*://[^:]+:([^@]+)@.*#\1#')
if [ -z "$PW" ] || [ ${#PW} -lt 8 ]; then
  echo "ABORT: パスワードを特定できないか短すぎます" >&2
  exit 1
fi

cp -p .env .env.bak
# 値を見せずに置換する（python が環境変数経由で受け取る）
PW="$PW" python3 - <<'EOF'
import os
pw = os.environ["PW"]
path = ".env"
lines = open(path).read().splitlines(keepends=True)
found = False
for i, l in enumerate(lines):
    if l.startswith("POSTGRES_PASSWORD="):
        lines[i] = "POSTGRES_PASSWORD=" + pw + "\n"
        found = True
if not found:
    lines.append("POSTGRES_PASSWORD=" + pw + "\n")
open(path, "w").writelines(lines)
print("updated POSTGRES_PASSWORD line (value not shown)")
EOF
echo "backup: .env.bak"
echo "NOTE: 実行中 DB の変更は不要（live は既にこの値）。volume 再作成時の整合用。"
