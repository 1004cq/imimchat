# CQIM 运行栈

旧 Mongo / MySQL / SQLite / COS 上的历史数据**不再迁移**。新库从空 PG 开始。

```text
PostgreSQL   账号消息会话 MediaFile
Redis        presence PubSub 网关
./data/media 图片语音视频圣纸文档
```

```bash
cd cqim-app
cp .env.example .env
docker compose up -d --build
```

起来后是空账号，需要重新注册。旧 COS 链接会失效。
