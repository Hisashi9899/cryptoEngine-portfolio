#!/usr/bin/env bash
# cryptoEngine ops — local SSH key + VPS-side setup helper.
#
#   1) creates a dedicated ed25519 keypair at ops/keys/cryptoengine_ops (no passphrase)
#   2) prints the PUBLIC key to install on the VPS
#   3) prints the exact VPS-side commands for the read-only collect user
#
# The collect user needs docker access for `docker compose logs` — grant the
# docker group only if you accept that tradeoff (docker group ~ root-equivalent).
set -euo pipefail

OPS_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KEY_DIR="$OPS_ROOT/keys"
KEY="$KEY_DIR/cryptoengine_ops"
USER_NAME="${COLLECT_USER:-cryptoengine-collect}"

mkdir -p "$KEY_DIR"
chmod 700 "$KEY_DIR"

if [ ! -f "$KEY" ]; then
  ssh-keygen -t ed25519 -N "" -C "cryptoengine-ops@local" -f "$KEY" >/dev/null
  echo "==> generated: $KEY"
else
  echo "==> key already exists: $KEY"
fi
chmod 600 "$KEY"
chmod 644 "$KEY.pub"

echo
echo "==> PUBLIC KEY (install on the VPS) =="
cat "$KEY.pub"
echo
echo "==> VPS-side setup (run as root ON the VPS) =="
cat <<EOF
# 1. create the read-only collect user
useradd -m -s /bin/bash $USER_NAME

# 2. install the public key (copy the line above)
mkdir -p /home/$USER_NAME/.ssh
echo '$(cat "$KEY.pub")' >> /home/$USER_NAME/.ssh/authorized_keys
chmod 700 /home/$USER_NAME/.ssh
chmod 600 /home/$USER_NAME/.ssh/authorized_keys
chown -R $USER_NAME:$USER_NAME /home/$USER_NAME/.ssh

# 3. read access to nginx logs (read-only)
usermod -aG adm $USER_NAME
# 4. docker logs access — only if you accept docker-group privileges:
usermod -aG docker $USER_NAME

# 5. staging dir (rsync pull target)
install -d -o $USER_NAME -g $USER_NAME -m 750 /home/$USER_NAME/logs

# 6. optional hardening: restrict to rsync+collect only (see ops/scripts/remote/collect-gate.sh)
EOF

echo
echo "==> After that, fill ops/scripts/config.env (VPS_HOST / COLLECT_USER / PROD_URL) and run:"
echo "    ops/scripts/collect.sh"
echo "    ops/scripts/healthcheck.sh"
