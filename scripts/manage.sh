#!/bin/bash
# imimchat 服务管理脚本
# 用途：快速管理所有 Docker 容器服务
# 使用方法：bash scripts/manage.sh [start|stop|restart|status|logs]

COMPOSE_DIR="/home/ubuntu/tsdd"

case "$1" in
  start)
    echo "启动所有服务..."
    cd "$COMPOSE_DIR" && docker compose up -d
    ;;
  stop)
    echo "停止所有服务..."
    cd "$COMPOSE_DIR" && docker compose down
    ;;
  restart)
    echo "重启所有服务..."
    cd "$COMPOSE_DIR" && docker compose down && docker compose up -d
    ;;
  restart-web)
    echo "重启 Web 前端服务..."
    docker restart tsdd-tangsengdaodaoweb-1
    ;;
  restart-api)
    echo "重启 API 服务..."
    docker restart tsdd-tangsengdaodaoserver-1
    ;;
  status)
    echo "=== 服务状态 ==="
    docker ps --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}' | grep tsdd
    echo ""
    echo "=== Nginx 状态 ==="
    systemctl status nginx --no-pager -l | head -10
    ;;
  logs)
    SERVICE="${2:-tangsengdaodaoserver}"
    echo "查看 $SERVICE 日志（最近 100 行）..."
    docker logs --tail=100 -f "tsdd-${SERVICE}-1"
    ;;
  update-web)
    echo "更新 Web 前端镜像..."
    cd "$COMPOSE_DIR"
    docker compose pull tangsengdaodaoweb
    docker compose up -d tangsengdaodaoweb
    echo "注意：更新后需要重新注入自定义文件！"
    echo "执行：bash scripts/deploy-frontend.sh"
    ;;
  *)
    echo "用法: $0 {start|stop|restart|restart-web|restart-api|status|logs [service]|update-web}"
    echo ""
    echo "命令说明："
    echo "  start         启动所有服务"
    echo "  stop          停止所有服务"
    echo "  restart       重启所有服务"
    echo "  restart-web   仅重启 Web 前端"
    echo "  restart-api   仅重启 API 服务"
    echo "  status        查看服务状态"
    echo "  logs [svc]    查看服务日志（默认：tangsengdaodaoserver）"
    echo "  update-web    更新 Web 前端镜像"
    exit 1
    ;;
esac
