# CQIM 运行栈

## 默认部署

默认 Compose 文件是 `cqim-app/docker-compose.yml`，只启动 `postgres`、`redis`、`cqim`、`go-gateway` 与 `nginx`。现网仍由 Node API、Go 网关和 Nginx 承载，不包含 C++ 服务。

旧 Mongo、MySQL、SQLite、COS 数据不迁移，新库从空 PostgreSQL 开始。起来后是空账号，需要重新注册；旧 COS 链接会失效。

```text
PostgreSQL   Prisma 主库：账号、消息、会话、MediaFile 元数据
Redis        presence、PubSub、网关共享状态
./data/media 图片、语音、视频、贴纸与文档二进制文件
```

## 数据库与媒体

Prisma provider 固定为 PostgreSQL，默认环境变量为：

```dotenv
DATABASE_URL=postgresql://cqim:cqim@postgres:5432/cqim?schema=public
REDIS_URL=redis://redis:6379
MEDIA_ROOT=/app/data/media
```

Node 容器启动时先执行 `pnpm exec prisma migrate deploy`；迁移失败会阻止 API 启动，不使用 `db push`，也不会忽略迁移错误。`MediaFile` 只保存元数据，包括 `kind`、`mime`、`diskPath`、`publicPath`、`durationMs`、`posterMediaId` 与 `sha256`，媒体文件实际写入 `MEDIA_ROOT`，不使用 BYTEA 保存整文件。

新的 `/api/media/upload` 与 `/api/media/upload-form` 均写入本地盘并创建 PostgreSQL 元数据，读取通过 `/api/media/:id`。历史 `/api/media/files/*` 静态路径保留作兼容。

`/api/cos/sts` 仅作历史兼容接口。缺少 COS 密钥、Bucket 或配置禁用时返回 HTTP `503`；新上传不依赖 COS，即使 COS 未配置也走本地盘。

## 部署与验收

```bash
cd cqim-app
cp .env.example .env
docker compose up -d --build
curl http://127.0.0.1/api/health
docker compose exec cqim pnpm exec prisma migrate deploy
```

`cqim-app/deploy/docker-compose.prod.yml` 与 `docker-compose.app.yml` 是可选的 Node/Go 部署覆盖，同样使用 PostgreSQL、Redis 与本地媒体盘，不引入 MongoDB、MySQL 或 SQLite。

## C++ 分支边界

C++ 迁移只在 `codex/wed-cpp-migration` 分支的 `wed-cpp/` 目录并行开发。默认 Compose、现网 Node API、Go 网关和 Nginx 不指向或依赖 C++ 服务。

本次栈调整不会自动修改现网 Nginx 上游、不会执行生产切流，也不会删除旧远程分支。应先在隔离环境完成健康检查与迁移验证，再由维护者手动安排切换窗口。
