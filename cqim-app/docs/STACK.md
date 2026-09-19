# CQIM 运行栈（已落地）

```text
PostgreSQL  用户/消息/MediaFile 元数据
Redis       presence / PubSub / 网关
./data/media  图片语音视频圣纸文档
```

启动：

```bash
cd cqim-app
cp .env.example .env   # 改密码
docker compose up -d --build
```

容器起来后 API 会尝试 `prisma migrate deploy`。
旧上传仍可走 `POST /api/media/upload`（index 里已有本地盘回退）与 `media-router`。

不要再把 DATABASE_URL 写成 mongodb 或 file:./dev.db。
Mongo/MySQL：`docker compose --profile legacy up -d`
