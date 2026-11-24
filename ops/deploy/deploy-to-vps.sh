#!/usr/bin/env bash
# cryptoEngine 初期デプロイ（WSL で実行）
#
#   ops/deploy/deploy-to-vps.sh [--dry-run]
#
# やること:
#   1) SSH 接続確認（setup-ssh.sh 済みが前提）
#   2) VPS 側に /opt/cryptoengine を作成
#   3) rsync で転送（.env 系・秘密鍵・ビルド成果物は除外）
#
# 安全装置:
#   - 除外リストは ops/deploy/rsync-excludes.txt に一元化（sync-master.sh と共有）
#   - 転送直前に除外漏れを自己検査（.env 実ファイルが含まれていたら中断）
#   - --dry-run で事前確認可能
set -euo pipefail

# ---- 固定パラメータ（環境変数で上書き可能） ----
VPS_IP="${VPS_IP:-203.0.113.10}"
VPS_USER="${VPS_USER:-deploy}"
REMOTE_DIR="${REMOTE_DIR:-/opt/cryptoengine}"
SSH_ALIAS="${SSH_ALIAS:-cryptoengine-vps}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
EXCLUDES="$SCRIPT_DIR/rsync-excludes.txt"

DRY_RUN=""
if [ "${1:-}" = "--dry-run" ]; then
  DRY_RUN="--dry-run"
  echo "==> DRY-RUN mode (no changes)"
fi

SSH_OPTS="-o BatchMode=yes -o StrictHostKeyChecking=accept-new"

echo "==> [1/4] SSH 接続確認"
ssh $SSH_OPTS "$SSH_ALIAS" 'echo "SSH_OK $(hostname)"'

echo "==> [2/4] 除外漏れの自己検査（.env 実ファイルが含まれないこと）"
# rsync の転送対象リストを dry-run で取得し、秘密ファイルが混じっていないか検査
CHECK_DIR=$(mktemp -d)
trap 'rm -rf "$CHECK_DIR"' EXIT
# ディレクトリエントリ（末尾/）と *.example・*.pub は許可するので除外して判定
# （ディレクトリ自体に中身はなく、配下ファイルは個別に検査される）
LEAKS=$(rsync -az --dry-run --out-format='%n' \
  --include='.env.example' \
  --include='*.pub' \
  --exclude-from="$EXCLUDES" \
  "$REPO_ROOT/" "$CHECK_DIR/" 2>/dev/null \
  | grep -E '(^|/)\.env(\.|$)|config\.env$|/keys/[^.]*$|\.pem$|\.key$' || true)
LEAKS=$(echo "$LEAKS" | grep -v '/$' | grep -v '\.example$' | grep -v '\.pub$' || true)
if [ -n "$LEAKS" ]; then
  echo "ABORT: 秘密ファイルが転送対象に含まれています:" >&2
  echo "$LEAKS" >&2
  exit 1
fi
echo "secret check: OK (.env 実体・秘密鍵なし)"

echo "==> [3/4] VPS 側ディレクトリ作成"
ssh $SSH_OPTS "$SSH_ALIAS" "mkdir -p '$REMOTE_DIR' && chmod 755 '$REMOTE_DIR'"

echo "==> [4/4] rsync 転送"
rsync -avz --delete $DRY_RUN \
  --include='.env.example' \
  --include='*.pub' \
  --exclude-from="$EXCLUDES" \
  -e "ssh $SSH_OPTS" \
  "$REPO_ROOT/" "$SSH_ALIAS:$REMOTE_DIR/"

# 固定deny設定はリポジトリ側から毎回配置する（trade_logs は --delete 対象外）。
ssh $SSH_OPTS "$SSH_ALIAS" \
  "cp '$REMOTE_DIR/ops/deploy/vt_static_deny.json' '$REMOTE_DIR/backend/core/trade_logs/vt_static_deny.json'"

echo "==> deploy-to-vps.sh 完了"
echo "次: VPS 上で bash $REMOTE_DIR/ops/deploy/setup-vps.sh を実行"
echo "（または ssh $SSH_ALIAS 'bash $REMOTE_DIR/ops/deploy/setup-vps.sh'）"
