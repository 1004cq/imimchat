# 媒体存储（不用 COS）

私有仓。图片 / 语音 / 圣纸 / 文档 **不进 PostgreSQL 当 BLOB**。
库只存元数据；文件落本机盘或自建 MinIO（S3 协议，替代腾讯云 COS）。

## 为什么不是「一个库装文件」

- PG 备份会起几十 GB
- 拉历史要把二进制拖出来
- 无法 Nginx 直出 / 缓存
OpenIM 也是 Mongo 存 URL + MinIO 存文件。

## 推荐结构

```text
客户端 POST /api/media/upload
  → 服务端写入 data/media/{kind}/{yyyy}/{id}
  → Prisma MediaAsset 一行
  → 返回 /media/{id}
消息里只存 mediaId 或 /media/{id}
加密文件：盘上是密文，库里可存 keyId（不存明文密钥）
```

目录例：

```text
data/media/
  image/
  voice/
  sticker/
  file/
```

## Prisma 模型（让 Codex 合进 schema.prisma）

```prisma
enum MediaKind {
  image
  voice
  sticker
  file
}

model MediaAsset {
  id          String    @id @default(cuid())
  ownerId     String
  kind        MediaKind
  mime        String
  size        Int
  width       Int?
  height      Int?
  durationMs  Int?
  sha256      String?
  diskPath    String    // 相对 data/media/...
  publicPath  String    // /media/{id}
  createdAt   DateTime  @default(now())

  owner User @relation(fields: [ownerId], references: [id])

  @@index([ownerId, createdAt])
  @@index([kind, createdAt])
}
```

User 上要加 `mediaAssets MediaAsset[]`。

## 环境变量

```bash
MEDIA_ROOT=./data/media
MEDIA_PUBLIC_PREFIX=/media
# 不再需要 COS_SECRET_ID / COS_BUCKET
```

双机：两台不要各写一份盘。要么只一台提供 /media，要么换 MinIO 单实例共享。
不要 NFS 共享 SQLite 那套搞法共享媒体目录当主库。

## 不要做

- 不要 BYTEA 存语音/图
- 不要 GridFS / Mongo 当 Prisma 主库
- 不要把加密私钥写进 MediaAsset
