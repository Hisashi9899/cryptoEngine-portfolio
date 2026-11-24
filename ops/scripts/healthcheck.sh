#!/usr/bin/env bash
# cryptoEngine ops — post-deploy health checks, output as Markdown.
#
#   ops/scripts/healthcheck.sh
#
# Checks (remote via SSH): containers, api /health, core gRPC port, db pg_isready, system.
# Checks (local HTTP):      frontend /login, bike.glb Content-Length.
# Output: ops/reports/health-YYYYMMDD.md  (template sections for Copilot/OpenCode).
set -uo pipefail

OPS_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG="$OPS_ROOT/scripts/config.env"
if [ ! -f "$CONFIG" ]; then
  echo "config.env not found — cp scripts/config.env.example scripts/config.env and fill it in." >&2
  exit 1
fi
set -a; source "$CONFIG"; set +a

SSH_DEST="${COLLECT_USER}@${VPS_HOST}"
SSH_OPTS="-i $SSH_KEY -p $SSH_PORT -o BatchMode=yes -o ConnectTimeout=10"
DATE="$(date +%Y%m%d)"
REPORT="$OPS_ROOT/reports/health-$DATE.md"
mkdir -p "$OPS_ROOT/reports"

# ---------- remote checks ----------
REMOTE="$(ssh $SSH_OPTS "$SSH_DEST" \
  "COMPOSE_DIR='$COMPOSE_DIR' COMPOSE_FILE='$COMPOSE_FILE' API_HEALTH_URL='$API_HEALTH_URL' bash -s" <<'EOF' 2>&1 || echo "__SSH_FAIL__"
COMPOSE="docker compose -f $COMPOSE_DIR/$COMPOSE_FILE"
echo "===CONTAINERS==="
$COMPOSE ps 2>&1 || true
echo "===API==="
curl -s -m 5 -o /dev/null -w "http_code=%{http_code}" "$API_HEALTH_URL"; echo
curl -s -m 5 "$API_HEALTH_URL" 2>/dev/null; echo
echo "===CORE==="
timeout 2 bash -c '</dev/null >/dev/tcp/localhost/50051' 2>/dev/null && echo "tcp 50051 OK" || echo "tcp 50051 FAIL"
echo "===DB==="
$COMPOSE exec -T db pg_isready -U crypto -d crypto_engine 2>&1 || true
echo "===SYS==="
uptime
free -m | head -3
df -h / | tail -1
echo "===DOCKERPS==="
docker ps --format '{{.Names}}\t{{.Status}}' 2>/dev/null || true
EOF
)"

# ---------- local HTTP checks ----------
FRONT_HTTP="$(curl -s -m 8 -o /dev/null -w '%{http_code}' "$PROD_URL/login" || echo 'ERR')"
GLB_HTTP="$(curl -s -m 8 -o /dev/null -w '%{http_code}' "$PROD_URL$GLB_PATH" || echo 'ERR')"
GLB_LEN="$(curl -s -m 8 -I "$PROD_URL$GLB_PATH" 2>/dev/null | tr -d '\r' | awk -F': ' 'tolower($1)=="content-length" {print $2; exit}')"
if [ -n "$GLB_LEN" ] && [ "$GLB_LEN" = "$EXPECTED_GLB_SIZE" ]; then
  GLB_RESULT="OK"
elif [ -z "$GLB_LEN" ]; then
  GLB_RESULT="NO-LENGTH"
else
  GLB_RESULT="MISMATCH (got $GLB_LEN, expected $EXPECTED_GLB_SIZE)"
fi

# ---------- render report ----------
cat > "$REPORT" <<EOF
# Health Report: $DATE

## サマリ（Copilot 用）
（ここに Copilot / OpenCode が 1〜3 行でサマリを記入）

## チェック結果
| 対象 | 結果 | 詳細 |
|---|---|---|
| frontend /login | $FRONT_HTTP | $PROD_URL/login |
| 3D モデル bike.glb | $GLB_HTTP / $GLB_RESULT | $PROD_URL$GLB_PATH (expect ${EXPECTED_GLB_SIZE} B) |
| SSH (VPS 到達性) | \`$([ "${REMOTE#*__SSH_FAIL__}" = "$REMOTE" ] && echo OK || echo FAIL)\` | $SSH_DEST |

### VPS 側チェック出力
\`\`\`text
$REMOTE
\`\`\`

## 検出された問題（OpenCode 用）
- （なし / エラー・5xx・DOWN がここに並ぶ。ログは ops/logs/$DATE/ を参照）

## 改善案（提案のみ・適用は禁止）
- [ ] （提案候補）

## 適用判断
- [ ] 保留
- [ ] 適用
- [ ] 却下

---
*生成: $(date -Iseconds) by ops/scripts/healthcheck.sh*
EOF

echo "==> report written: $REPORT"
grep -n '^| ' "$REPORT" | sed -n '1,8p'
