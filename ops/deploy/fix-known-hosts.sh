#!/usr/bin/env bash
# known_hosts の古いホスト鍵を安全に除去し、新鍵を登録し直す（WSL で実行）
#
#   ops/deploy/fix-known-hosts.sh [--yes] [EXPECTED_FINGERPRINT=SHA256:...]
#
# 前提: VPS を自分で再構築した等、ホスト鍵の変更が正当であることが確定していること。
# 身に覚えがない変更は続行せず、Cloud コンソールで正規指紋と照合すること。
#
# やること:
#   1) known_hosts をバックアップ（復元可能にする）
#   2) 対象 IP のエントリだけを表示・削除（他ホストには触らない）
#   3) 新ホスト鍵を取得して指紋を表示（EXPECTED_FINGERPRINT 指定時は厳密照合）
#   4) ~/.ssh/config の整合性確認
#   5) setup-ssh.sh を自動再実行（公開鍵登録〜接続テスト）
#
# 使用例:
#   bash ops/deploy/fix-known-hosts.sh
#   EXPECTED_FINGERPRINT=SHA256:xxxx bash ops/deploy/fix-known-hosts.sh --yes
set -euo pipefail

# ---- 固定パラメータ（環境変数で上書き可能） ----
VPS_IP="${VPS_IP:-203.0.113.10}"
VPS_USER="${VPS_USER:-deploy}"
SSH_ALIAS="${SSH_ALIAS:-cryptoengine-vps}"
SSH_PORT="${SSH_PORT:-22}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KNOWN_HOSTS="$HOME/.ssh/known_hosts"
YES=0
[ "${1:-}" = "--yes" ] && YES=1
# 任意: Cloud コンソールで確認した正規指紋（例: EXPECTED_FINGERPRINT=SHA256:xxxx）
EXPECTED="${EXPECTED_FINGERPRINT:-}"

echo "==> [1/6] known_hosts をバックアップ"
[ -f "$KNOWN_HOSTS" ] || { echo "ABORT: $KNOWN_HOSTS がありません" >&2; exit 1; }
BACKUP="$KNOWN_HOSTS.bak.$(date +%Y%m%d%H%M%S)"
cp -p "$KNOWN_HOSTS" "$BACKUP"
chmod 600 "$BACKUP"
echo "backup: $BACKUP"

echo "==> [2/6] 対象 IP の登録状況を確認"
echo "--- $VPS_IP に関するエントリ（削除対象候補） ---"
ssh-keygen -F "$VPS_IP" -f "$KNOWN_HOSTS" || echo "(not found — すでに除去済みの可能性)"
echo "--- 参考: 別ホストのエントリ数（触らない） ---"
grep -vc . "$KNOWN_HOSTS" || true

echo "==> [3/6] 対象 IP の古い鍵だけを削除"
# ssh-keygen -R は平文・ハッシュ化の両形式の該当エントリのみ除去する
ssh-keygen -f "$KNOWN_HOSTS" -R "$VPS_IP"
# ポート付き形式 [IP]:port の残骸も除去（通常は不要だが念のため）
ssh-keygen -f "$KNOWN_HOSTS" -R "[$VPS_IP]:$SSH_PORT" 2>/dev/null || true
chmod 600 "$KNOWN_HOSTS"
if ssh-keygen -F "$VPS_IP" -f "$KNOWN_HOSTS" >/dev/null 2>&1; then
  echo "ABORT: 除去に失敗しました。$BACKUP から復元してください" >&2
  exit 1
fi
echo "removed: $VPS_IP のエントリを削除しました"

echo "==> [4/6] 新ホスト鍵を取得して指紋を表示"
NEW_KEYS="$(mktemp)"
trap 'rm -f "$NEW_KEYS"' EXIT
ssh-keyscan -t ed25519 -p "$SSH_PORT" "$VPS_IP" > "$NEW_KEYS" 2>/dev/null
if [ ! -s "$NEW_KEYS" ]; then
  echo "ABORT: ホスト鍵を取得できません（VPS の sshd を確認）。復元: cp $BACKUP $KNOWN_HOSTS" >&2
  exit 1
fi
echo "--- 新ホスト鍵の指紋（必ず目視確認） ---"
ssh-keygen -lf "$NEW_KEYS"
NEW_FP="$(ssh-keygen -lf "$NEW_KEYS" | awk '{print $2}')"
if [ -n "$EXPECTED" ]; then
  if [ "$NEW_FP" = "$EXPECTED" ]; then
    echo "fingerprint: MATCH ($EXPECTED)"
  else
    echo "ABORT: 指紋不一致 (got=$NEW_FP want=$EXPECTED)" >&2
    echo "復元: cp $BACKUP $KNOWN_HOSTS" >&2
    exit 1
  fi
elif [ "$YES" != "1" ]; then
  echo "上記指紋が Cloud コンソール上の正規指紋と一致することを確認してください。"
  echo "VPS コンソールでの確認コマンド: ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub"
  printf "続行しますか? [y/N] "
  read -r ans
  [ "$ans" = "y" ] || [ "$ans" = "Y" ] || { echo "中断しました（known_hosts は除去済み・バックアップあり）"; exit 1; }
else
  echo "注意: --yes のため目視確認をスキップしました（指紋は上記ログに記録）"
fi
# 確認済みの新鍵を known_hosts に登録（次回以降の strict 検証用）
cat "$NEW_KEYS" >> "$KNOWN_HOSTS"
chmod 600 "$KNOWN_HOSTS"
echo "registered: 新ホスト鍵を $KNOWN_HOSTS に登録しました"

echo "==> [5/6] ~/.ssh/config の整合性確認"
if grep -qE "^Host[[:space:]]+$SSH_ALIAS([[:space:]]|$)" "$HOME/.ssh/config" 2>/dev/null; then
  echo "Host $SSH_ALIAS: OK"
else
  echo "Host $SSH_ALIAS がないため setup-ssh.sh 側で再作成されます"
fi

echo "==> [6/6] setup-ssh.sh を自動再実行（公開鍵登録〜接続テスト）"
bash "$SCRIPT_DIR/setup-ssh.sh"

echo "==> fix-known-hosts.sh 完了"
echo "バックアップは $BACKUP に残っています（不要になれば削除）"
