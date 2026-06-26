#!/usr/bin/env bash
# 部署 NeoMsg MTProto 2.0 网关（或可选 Teamgram 全栈）
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEPLOY="${ROOT}/neomsg/deploy"
MODE="${1:-neomsg}"

echo "==> imimchat MTProto 部署模式: ${MODE}"

case "${MODE}" in
  neomsg)
    cd "${DEPLOY}"
    if [[ ! -f .env ]]; then
      cp .env.example .env
      echo "已创建 ${DEPLOY}/.env，请按需修改"
    fi
    docker compose -f docker-compose.yaml -f docker-compose.mtproto.yaml up -d --build
    echo ""
    echo "NeoMsg MTProto 网关: tcp://localhost:10443"
    echo "健康检查: http://localhost:10444/health"
    ;;
  teamgram)
    TEAMGRAM_DIR="${DEPLOY}/teamgram"
    if [[ ! -d "${TEAMGRAM_DIR}/teamgram-server" ]]; then
      echo "克隆 Teamgram 服务端..."
      git clone --depth 1 https://github.com/teamgram/teamgram-server.git "${TEAMGRAM_DIR}/teamgram-server"
    fi
    cd "${TEAMGRAM_DIR}/teamgram-server"
    docker compose up -d --build
    echo ""
    echo "Teamgram MTProto: tcp://localhost:10443"
    echo "需配合 Teamgram 客户端（Android/iOS/Desktop）使用"
  ;;
  *)
    echo "用法: $0 [neomsg|teamgram]"
    exit 1
    ;;
esac
