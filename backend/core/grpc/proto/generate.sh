#!/usr/bin/env bash
# engine.proto から Go / Python の gRPC スタブを再生成する。
#
# 前提:
#   - protoc が PATH にある、または python3 に grpcio-tools が入っている
#     （python3 -m grpc_tools.protoc が使える場合はこちらを優先）
#   - protoc-gen-go と protoc-gen-go-grpc が PATH にある
#     （未導入の場合: go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
#                     go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest）
#
# 使い方:
#   ./generate.sh
#
# 生成先:
#   Go:     backend/core/grpc/rpcgen/*.go
#   Python: backend/api/engine_pb2*.py

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROTO_DIR="$SCRIPT_DIR"
CORE_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
API_DIR="$(cd "$CORE_DIR/../api" && pwd)"
PROTO_FILE="$PROTO_DIR/engine.proto"

# protoc の解決（環境変数 PROTOC を最優先、次に grpc_tools.protoc、最後に protoc）
if [ -n "${PROTOC:-}" ]; then
  PROTOC_ARGS=($PROTOC)
elif python3 -c 'import grpc_tools.protoc' >/dev/null 2>&1; then
  PROTOC_ARGS=(python3 -m grpc_tools.protoc)
else
  PROTOC_ARGS=(protoc)
fi

# Go プラグインを PATH へ
export PATH="$HOME/go/bin:$PATH"

cd "$CORE_DIR"

# Go スタブ（github.com/cryptoEngine/core/grpc/rpcgen）
"${PROTOC_ARGS[@]}" \
  -I "$PROTO_DIR" \
  --go_out="$CORE_DIR" --go_opt=module=github.com/cryptoEngine/core \
  --go-grpc_out="$CORE_DIR" --go-grpc_opt=module=github.com/cryptoEngine/core \
  "$PROTO_FILE"

# Python スタブ（backend/api/engine_pb2*.py）
"${PROTOC_ARGS[@]}" \
  -I "$PROTO_DIR" \
  --python_out="$API_DIR" --grpc_python_out="$API_DIR" \
  "$PROTO_FILE"

echo "gRPC stubs regenerated:"
echo "  Go:     $CORE_DIR/grpc/rpcgen/"
echo "  Python: $API_DIR/engine_pb2.py, $API_DIR/engine_pb2_grpc.py"
