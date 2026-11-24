#!/usr/bin/env bash
# cryptoEngine デプロイ用 SSH 設定の自動化（WSL で実行）
#
#   ops/deploy/setup-ssh.sh
#
# やること:
#   1) デプロイ専用 ed25519 鍵を作成（ops/keys/cryptoengine_deploy）
#      ※ 既存の collect 用鍵（cryptoengine_ops）とは分離する
#   2) ~/.ssh の権限を正規化
#   3) ~/.ssh/config に Host エントリを自動生成（存在すれば追記しない）
#   4) ssh-copy-id で公開鍵を VPS に登録（初回のみ VPS パスワードが必要）
#   5) BatchMode で接続テスト
#
# 使い方: bash ops/deploy/setup-ssh.sh
set -euo pipefail

# ---- 固定パラメータ（環境変数で上書き可能） ----
VPS_IP="${VPS_IP:-203.0.113.10}"
VPS_USER="${VPS_USER:-deploy}"
SSH_ALIAS="${SSH_ALIAS:-cryptoengine-vps}"
SSH_PORT="${SSH_PORT:-22}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
KEY_DIR="$REPO_ROOT/ops/keys"
KEY="$KEY_DIR/cryptoengine_deploy"
SSH_DIR="$HOME/.ssh"
SSH_CONFIG="$SSH_DIR/config"

echo "==> [1/5] SSH鍵の作成確認"
mkdir -p "$KEY_DIR"
chmod 700 "$KEY_DIR"
if [ ! -f "$KEY" ]; then
  # -N ""（パスフレーズなし＝無人デプロイ用。鍵ファイル自体の管理に注意）
  ssh-keygen -t ed25519 -N "" -C "cryptoengine-deploy@local" -f "$KEY" >/dev/null
  echo "generated: $KEY"
else
  echo "key already exists: $KEY (reuse)"
fi
chmod 600 "$KEY"
chmod 644 "$KEY.pub"

echo "==> [2/5] ~/.ssh の権限を正規化"
mkdir -p "$SSH_DIR"
chmod 700 "$SSH_DIR"
# config があれば 600 に（なければ後で作る）
[ -f "$SSH_CONFIG" ] && chmod 600 "$SSH_CONFIG" || true

echo "==> [3/5] ~/.ssh/config に Host エントリを追加"
if grep -qE "^Host[[:space:]]+$SSH_ALIAS([[:space:]]|$)" "$SSH_CONFIG" 2>/dev/null; then
  echo "Host $SSH_ALIAS already exists in $SSH_CONFIG (skip)"
else
  cat >> "$SSH_CONFIG" <<EOF

Host $SSH_ALIAS
  HostName $VPS_IP
  User $VPS_USER
  Port $SSH_PORT
  IdentityFile $KEY
  IdentitiesOnly yes
EOF
  chmod 600 "$SSH_CONFIG"
  echo "added Host $SSH_ALIAS to $SSH_CONFIG"
fi

echo "==> [4/5] 公開鍵を VPS に登録"
# 既に鍵認証できるか先に確認（できれば登録スキップ）
if ssh -o BatchMode=yes -o ConnectTimeout=10 "$SSH_ALIAS" true 2>/dev/null; then
  echo "key already registered (BatchMode OK, skip ssh-copy-id)"
else
  # ホスト鍵不一致（VPS 再構築等）は ssh-copy-id 前に検出して安全に停止する。
  # 勝手に known_hosts を書き換えない。対処は fix-known-hosts.sh に委譲。
  if ssh -o BatchMode=yes -o ConnectTimeout=10 \
      -o StrictHostKeyChecking=yes "$VPS_USER@$VPS_IP" true 2>&1 \
      | grep -q "REMOTE HOST IDENTIFICATION HAS CHANGED"; then
    echo "ABORT: known_hosts のホスト鍵が VPS と不一致です。" >&2
    echo "VPS を再構築した場合は正常な現象です。以下で修復してください:" >&2
    echo "  bash ops/deploy/fix-known-hosts.sh" >&2
    exit 1
  fi
  echo "初回のみ VPS のログインパスワードを聞かれます"
  ssh-copy-id -i "$KEY.pub" -p "$SSH_PORT" "$VPS_USER@$VPS_IP"
fi

echo "==> [5/5] 接続テスト (BatchMode)"
ssh -o BatchMode=yes "$SSH_ALIAS" 'echo "SSH_OK $(hostname) $(whoami)"'
echo "==> setup-ssh.sh 完了。次: bash ops/deploy/deploy-to-vps.sh"
