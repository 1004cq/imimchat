# 媒体存储（MinIO）

图片、语音、视频、贴纸和文档**不进 PostgreSQL 当 BLOB**。文件统一写入 MinIO，PostgreSQL 只保存 `MediaFile` 元数据。

## 上传路径

```text
POST /api/media/upload       kind=image|voice|video|sticker|file
POST /api/media/upload-form  FormData 上传
POST /api/voice/upload       用户语音兼容入口
  → MinIO: media/{kind}/{yyyy}/{id}
  → MediaFile 一行
  → /api/media/{id}
```

视频额外信息可以写入 `durationMs`、`posterMediaId`；加密文件在 MinIO 中保存密文，服务端不解密或转码整段内容。

## Prisma MediaFile

```prisma
model MediaFile {
  id            String   @id
  userId        String?
  type          String
  kind          String   @default("file")
  url           String
  filename      String?
  mime          String?
  size          Int?
  width         Int?
  height        Int?
  durationMs    Int?
  posterMediaId String?
  sha256        String?
  diskPath      String?  // MinIO object key，非本地文件路径
  publicPath    String?  // /api/media/:id
  createdAt     DateTime @default(now())
}
```

## 环境变量

```dotenv
MINIO_ENDPOINT=minio
MINIO_PORT=9000
MINIO_USE_SSL=false
MINIO_ROOT_USER=cqimminio
MINIO_ROOT_PASSWORD=replace-with-a-strong-secret
MINIO_BUCKET=cqim-media
MINIO_REGION=us-east-1
MEDIA_MAX_VIDEO_BYTES=104857600
```

默认 Compose 只在内部暴露 MinIO API `9000` 和 Console `9001`，不映射到公网。双机部署时，应用节点通过 `MINIO_ENDPOINT=NODE1_HOST` 连接数据节点 MinIO，MinIO API 端口只对可信应用节点开放。

## 不要

不要使用 BYTEA 存视频，不要使用 Mongo GridFS，不要把 MinIO 密钥写入数据库，也不要在 Node 容器中把媒体持久化到 `./data/media`。
