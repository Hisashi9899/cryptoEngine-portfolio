#!/usr/bin/env bash
# OPTIONAL hardening — runs ON the VPS as the collect user's SSH command wrapper.
#
# Wire it in the collect user's authorized_keys:
#   command="/usr/local/bin/collect-gate.sh",no-agent-forwarding,no-port-forwarding,no-X11-forwarding,no-pty ssh-ed25519 AAAA... cryptoengine-ops@local
#
# It only permits:
#   - the log dump (`bash -s` heredoc from ops/scripts/collect.sh)
#   - read-only rsync (--sender) used by the pull
# Everything else is denied.
set -u

case "${SSH_ORIGINAL_COMMAND:-}" in
  "bash -s")
    exec /bin/bash -s
    ;;
  rsync\ --server\ --sender*)
    exec /usr/bin/rsync $SSH_ORIGINAL_COMMAND
    ;;
  *)
    echo "denied: ${SSH_ORIGINAL_COMMAND:-<empty>}" >&2
    exit 1
    ;;
esac
