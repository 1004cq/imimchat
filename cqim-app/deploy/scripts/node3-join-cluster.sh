#!/usr/bin/env bash
# 在 103.24.217.244 上执行：停止单机栈，挂载 NFS，接入 node1 Redis 集群
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

NODE1_HOST="${NODE1_HOST:-42.194.167.201}"
SHARED_DATA_DIR="${SHARED_DATA_DIR:-/home/ubuntu/cqim_shared/data}"
NFS_EXPORT="${NODE1_HOST}:/home/ubuntu/cqim_shared/data"

echo "==> 停止单机 standalone 栈"
if [[ -f deploy/docker-compose.standalone.yml ]]; then
  docker compose -f deploy/docker-compose.standalone.yml down 2>/dev/null || true
fi

echo "==> 准备共享数据目录 ${SHARED_DATA_DIR}"
mkdir -p "$SHARED_DATA_DIR"

if ! mountpoint -q "$SHARED_DATA_DIR"; then
  echo "==> 挂载 NFS: ${NFS_EXPORT}"
  if ! mount -t nfs "$NFS_EXPORT" "$SHARED_DATA_DIR"; then
    echo "ERROR: NFS 挂载失败。请先在 node1 执行 deploy/scripts/node1-add-node3.sh" >&2
    exit 1
  fi
fi

if [[ ! -f "${SHARED_DATA_DIR}/cqim.db" ]]; then
  echo "ERROR: 共享库 ${SHARED_DATA_DIR}/cqim.db 不存在" >&2
  exit 1
fi

echo "==> 检查 node1 Redis"
if ! redis-cli -h "$NODE1_HOST" -p 6380 ping | grep -q PONG; then
  echo "ERROR: 无法连接 ${NODE1_HOST}:6380 Redis" >&2
  exit 1
fi

if [[ ! -f .env ]]; then
  cp deploy/env.node3.example .env
  echo "WARN: 已生成 .env，请填写与 node1 相同的 JWT_SECRET 后重新运行" >&2
  exit 1
fi

if grep -q 'REPLACE_WITH_NODE1_JWT_SECRET' .env 2>/dev/null; then
  echo "ERROR: .env 中 JWT_SECRET 仍为占位符，请从 node1 复制后重试" >&2
  exit 1
fi

echo "==> 构建并启动节点3 应用层"
if [[ -f deploy/standalone-up.sh ]]; then
  bash deploy/standalone-up.sh build cqim go-gateway 2>/dev/null || \
    docker compose -f deploy/docker-compose.node3.yml build cqim go-gateway
else
  docker compose -f deploy/docker-compose.node3.yml build cqim go-gateway
fi

docker compose -f deploy/docker-compose.node3.yml up -d --force-recreate

echo "==> 节点3 已启动"
docker compose -f deploy/docker-compose.node3.yml ps
curl -sf "http://127.0.0.1:3011/api/health" && echo
curl -sf "http://127.0.0.1:8082/health" && echo
