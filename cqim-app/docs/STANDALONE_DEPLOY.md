# 单机独立部署（与双节点生产环境隔离）

适用于在新 VPS 上单独运行 CQIM，**不**连接现有 node1/node2、NFS 共享库或外网 Redis。

## 要求

- Docker 24+ 与 Docker Compose v2
- 开放 **999** 端口（HTTP；宿主机 80 已被其他服务占用时可改用此端口）
- 建议 2GB+ 内存（首次 `docker compose build` 需要编译前端与镜像）

## 部署步骤

```bash
cd /opt/cqim-app
cp deploy/env.standalone.example .env
# 编辑 .env：PUBLIC_BASE_URL、CORS_ORIGINS、JWT_SECRET

docker compose -f deploy/docker-compose.standalone.yml up -d --build
docker compose -f deploy/docker-compose.standalone.yml ps
curl -s http://127.0.0.1:999/api/health
```

浏览器访问：`http://<服务器IP>:999/`

## 架构

```
用户 → Nginx:999 → cqim:3000（HTTP/WS /signal）
                → go-gateway:8081（/ws/group）
         Redis + SQLite（本地 Docker volume，不对外）
```

## 与 wed.imim.chat 双节点的区别

| 项目 | 单机 standalone | 双节点生产 |
|------|-----------------|------------|
| 数据库 | 本地 SQLite volume | NFS 共享 SQLite |
| Redis | 容器内 redis | node1 Redis + Pub/Sub 跨节点 |
| Gateway | 单实例 GATEWAY_ID | 两台 gw-app-1/2 |
| Nginx | 容器内 standalone 配置 | 宿主机 TLS + least_conn |
