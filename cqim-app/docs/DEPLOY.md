# CQIM 部署

生产只部署 GitHub `main`。  
不要合并 C++ 迁移分支、MTProto / NeoMsg / 第三方推送实验分支。  
`/api` 和 `/signal` 只指向 Node API 与 Go 网关，不要指向 C++。

## 域名

`PUBLIC_BASE_URL` 与 `CORS_ORIGINS` 必须和客户端实际请求的 HTTPS 来源一致。  
具体域名、解析与证书只写在部署机，不写进本文件。

源站已有 Nginx 时：不要覆盖原站点配置，用反代把站点、`/api`、`/signal` 转到本项目的 API 与网关容器。

## 栈

| 组件 | 作用 |
|------|------|
| PostgreSQL | 用户、会话、消息、朋友圈、管理日志、系统配置 |
| Redis | 在线状态、网关路由、Pub/Sub |
| MinIO | 图片、语音、视频、贴纸、文档 |
| Node | HTTP API |
| Go 网关 | WebSocket |
| Nginx | 入口 |

不使用 MongoDB、MySQL、第三方推送商。旧对象存储地址仅兼容读取，失败时不得让接口 500。

8 核 8G 可以先上线、做冒烟。同机还有其它站点时内存会紧。第一次构建镜像可能要几十分钟。

## 启动

```bash
cd cqim-app
git checkout main && git pull
cp .env.example .env
```

在 `.env` 填写数据库密码、MinIO 账号密码、`PUBLIC_BASE_URL` 与 `CORS_ORIGINS`。  
通话与推送密钥只放环境变量，不要写入 Git。

```bash
./scripts/deploy.sh
# 或：docker compose up -d --build && docker compose ps
```

顺序：Postgres / Redis / MinIO healthy → `prisma migrate deploy` → API 与网关 → Nginx。  
`DATABASE_URL` 必须是 PostgreSQL，禁止文件数据库路径。

## 探活

```bash
docker compose ps
curl -fsS https://你的域名/api/health
docker compose exec redis redis-cli ping
docker compose exec minio /usr/local/bin/busybox wget -q -O /dev/null \
  http://127.0.0.1:9000/minio/health/live
```

健康检查须显示 Redis 可用。MinIO 只在容器内探活，不要把对象存储控制台暴露到公网。

## 推送

仅前台在聊时跳过系统通知；长连接还在不算免推。  
按设备令牌环境分别发往对应推送网关。失效令牌只删对应一行。通知不含消息明文。

## 冒烟

1. 登录后刷新会话仍在
2. 加密私聊双方能解密
3. 结束进程后有系统通知（未真机验证不得写已通过）
4. 图片和语音可上传、刷新后仍可打开
5. 通话使用环境变量里的正式 AppId，禁止旧 ID 兜底
6. 二维码中的用户标识与头像正确
7. 从通知或联系人进入正确会话
8. 未读数字正确，进入会话后清零

## 更新

```bash
git checkout main && git pull
cd cqim-app && docker compose up -d --build
```

## 排障

```bash
docker compose logs --tail=200
docker compose exec cqim pnpm exec prisma migrate status
```
