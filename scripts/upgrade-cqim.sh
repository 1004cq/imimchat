#!/usr/bin/env bash
# imimchat CQIM 升级脚本 — 同步 Schema + 重建容器
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
echo "==> imimchat CQIM 升级"

echo "[1/4] 安装 cqim-app 依赖..."
cd "$ROOT/cqim-app"
if command -v pnpm >/dev/null 2>&1; then
  pnpm install --frozen-lockfile 2>/dev/null || pnpm install
else
  npm install
fi

echo "[2/4] 同步 Prisma Schema..."
if [ -f "$ROOT/docker/.env" ]; then
  set -a && source "$ROOT/docker/.env" && set +a
fi
export DATABASE_URL="${CQIM_SQLITE_URL:-file:./data/cqim.db}"
pnpm db:push

echo "[3/4] 重建 CQIM Docker 镜像..."
cd "$ROOT/docker"
docker compose build cqim

echo "[4/4] 重启 CQIM 服务..."
docker compose up -d cqim

echo ""
echo "✅ 升级完成"
echo "   健康检查: curl http://localhost:3000/api/health"
echo "   升级文档: docs/UPGRADE.md"
