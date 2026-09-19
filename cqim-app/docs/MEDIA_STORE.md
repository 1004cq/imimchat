# 媒体存储（不用 COS）

私有仓。图片 / 语音 / 视频 / 圣纸 / 文档 **不进 PostgreSQL 当 BLOB**。
库只存元数据；文件落本机盘或自建 MinIO。

## 推荐结构

```text
POST /api/media/upload  kind=image|voice|video|sticker|file
  → data/media/{kind}/{yyyy}/{id}
  → MediaAsset 一行
  → /media/{id}
视频额外：截一帧封面 → kind=image 或 posterMediaId
加密文件盘上是密文
```

```text
data/media/
  image/
  voice/
  video/
  sticker/
  file/
```

## Prisma

```prisma
enum MediaKind {
  image
  voice
  video
  sticker
  file
}

model MediaAsset {
  id            String    @id @default(cuid())
  ownerId       String
  kind          MediaKind
  mime          String
  size          Int
  width         Int?
  height        Int?
  durationMs    Int?      // 语音/视频
  posterMediaId String?   // 视频封面 MediaAsset.id
  sha256        String?
  diskPath      String
  publicPath    String
  createdAt     DateTime  @default(now())

  owner User @relation(fields: [ownerId], references: [id])

  @@index([ownerId, createdAt])
  @@index([kind, createdAt])
}
```

视频限制建议：mp4/webm，单文件先 100MB，超限拒绝。不要在 Node 里转码整段影片；封面用 ffmpeg 抽一帧即可。

## 环境

```bash
MEDIA_ROOT=./data/media
MEDIA_PUBLIC_PREFIX=/media
MEDIA_MAX_VIDEO_BYTES=104857600
```

双机只一台提供 /media 或共用 MinIO。

## 不要

BYTEA 存视频、Mongo GridFS、把密钥写进表。
