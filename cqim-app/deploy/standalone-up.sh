#!/usr/bin/env bash
# 单机部署构建脚本：临时允许 dist 进入 Docker 构建上下文
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if [[ ! -f dist/index.js ]]; then
  echo "请先在本机构建: npm run build" >&2
  exit 1
fi

if [[ -f .dockerignore ]]; then
  cp .dockerignore .dockerignore.standalone.bak
  grep -v '^dist$' .dockerignore.standalone.bak > .dockerignore
fi

cleanup() {
  if [[ -f .dockerignore.standalone.bak ]]; then
    mv .dockerignore.standalone.bak .dockerignore
  fi
}
trap cleanup EXIT

docker compose -f deploy/docker-compose.standalone.yml "$@"
