#!/usr/bin/env bash
# 生成 NeoMsg Protobuf Go 代码
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PROTO_DIR="${ROOT}/neomsg/proto"
OUT_DIR="${ROOT}/neomsg/backend/internal/protocol/pb"

mkdir -p "${OUT_DIR}"

if ! command -v protoc >/dev/null 2>&1; then
  echo "安装 protoc..."
  sudo apt-get update -qq && sudo apt-get install -y -qq protobuf-compiler
fi

export PATH="${PATH}:$(go env GOPATH)/bin"
if ! command -v protoc-gen-go >/dev/null 2>&1; then
  go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
fi

protoc \
  --proto_path="${PROTO_DIR}" \
  --go_out="${OUT_DIR}" \
  --go_opt=paths=source_relative \
  "${PROTO_DIR}/neomsg/v1/"*.proto

echo "Generated Go protobuf -> ${OUT_DIR}"
