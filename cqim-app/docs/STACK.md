# CQIM 运行栈

## 默认部署

默认 Compose 文件是 `cqim-app/docker-compose.yml`，启动 `postgres`、`redis`、`minio`、`cqim`、`go-gateway` 与 `nginx`。现网仍由 Node API、Go 网关和 Nginx 承载，不包含 C++ 服务，也不会自动生产切流。

旧 Mongo、MySQL、SQLite、COS 数据不迁移，新库从空 PostgreSQL 开始。起来后是空账号，需要重新注册；旧 COS 链接不再作为新上传入口。

```text
PostgreSQL   用户、好友、群、消息、会话、MediaFile 元数据、推送 token
Redis        在线 presence、网关路由、Pub/Sub、短缓存
MinIO        图片、语音、视频、贴纸、文档等二进制对象
```

## 数据库与对象存储

Prisma provider 固定为 PostgreSQL，默认环境变量为：

```dotenv
DATABASE_URL=postgresql://cqim:cqim@postgres:5432/cqim?schema=public
REDIS_URL=redis://redis:6379
MINIO_ENDPOINT=minio
MINIO_PORT=9000
MINIO_USE_SSL=false
MINIO_ROOT_USER=cqimminio
MINIO_ROOT_PASSWORD=cqimio-secret-change-me
MINIO_BUCKET=cqim-media
MINIO_REGION=us-east-1
```

Node 容器启动时先执行 `pnpm exec prisma migrate deploy`，随后连接 Redis 并确保 MinIO bucket 存在；任一步失败都会阻止 API 启动。应用不再创建或依赖 `./data/media` 本地媒体目录。

`MediaFile` 只保存对象元数据，包括 `kind`、`mime`、`diskPath`、`publicPath`、`durationMs`、`posterMediaId` 与 `sha256`。其中 `diskPath` 是 MinIO object key，不是本地文件路径；PostgreSQL 不使用 BYTEA 保存整文件。

新的媒体入口如下：

```text
POST /api/media/upload       Base64 上传
POST /api/media/upload-form  FormData 上传
GET  /api/media/:id          认证后从 MinIO 流式读取
POST /api/voice/upload       用户语音写入 MinIO
```

朋友圈、聊天图片/视频/文件、用户语音和机器人 TTS 语音均通过服务端 MinIO 适配层写入对象存储。前端不再向 COS 请求 STS，也不再直传 COS。

## MinIO 部署边界

默认 Compose 只在内部暴露 MinIO API `9000` 和 Console `9001`，不直接映射到宿主机公网端口。生产环境应通过内网或受控管理网络访问 Console，并将 `MINIO_ROOT_PASSWORD` 替换为强随机密钥。

节点应用覆盖 `docker-compose.app.yml` 不在节点2 启动 MinIO，而是通过 `MINIO_ENDPOINT=NODE1_HOST` 连接数据节点的 MinIO API。节点1 必须将 API 端口仅开放给可信应用节点。

## COS 兼容行为

`/api/cos/sts` 现在固定返回 HTTP `503`，提示使用 MinIO；旧 COS proxy 与旧本地媒体上传处理器已从现网 Node 路由移除。历史 COS 对象不迁移。

## 部署与验收

```bash
cd cqim-app
cp .env.example .env
# 修改 MINIO_ROOT_USER / MINIO_ROOT_PASSWORD 等敏感配置
./scripts/deploy.sh
# 或：docker compose up -d --build
curl -fsS http://127.0.0.1/api/health
```

`cqim` 容器启动时已经执行 `prisma migrate deploy`，不必再手工跑一遍。

应确认以下服务均健康：

```bash
docker compose ps postgres redis minio cqim go-gateway nginx
```

`cqim-app/deploy/docker-compose.prod.yml` 和 `docker-compose.app.yml` 是可选部署覆盖，同样使用 PostgreSQL、Redis 和 MinIO，不引入 MongoDB、MySQL、SQLite 或 C++ 服务。

## C++ 分支边界

C++ 迁移只在 `codex/wed-cpp-migration` 分支的 `wed-cpp/` 目录并行开发。默认 Compose、现网 Node API、Go 网关和 Nginx 不指向或依赖 C++ 服务。
