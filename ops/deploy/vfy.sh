#!/usr/bin/env bash
set -u
echo "=== md5 local ==="
md5sum /opt/cryptoengine/backend/core/capture/capture.go
echo "=== md5 remote ==="
ssh cryptoengine-vps 'md5sum /opt/cryptoengine/backend/core/capture/capture.go'
echo "=== trade_log rows before ==="
ssh cryptoengine-vps 'wc -l /opt/cryptoengine/backend/core/trade_logs/trade_log.csv'
