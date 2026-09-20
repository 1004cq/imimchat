# imimchat / CQIM

[English](./README_en.md) | **中文**

私有化 IM。产品代码在 `cqim-app/`。iOS 原生客户端不在本仓。

传输：HTTPS JSON + WebSocket。不做 MTProto / TDLib / 多 DC。
WuKongIM 不是主路径。

生产域名、证书与密钥只写在部署机 `.env`，不写进本说明。

---

## 技术栈

| 层 | 用什么 |
| --- | --- |
| Web | React + Vite（`cqim-app/client`） |
| API | Node + Express（`cqim-app/server`） |
| 网关 | Go（`cqim-app/go-gateway`） |
| 主库 | PostgreSQL（Prisma） |
| 缓存 / 在线 | Redis |
| 媒体 | MinIO（库中只存元数据） |
| 私聊 | 强制 `msgType=encrypted` |
| 推送 | 自建 APNs + Web Push |
| 通话 | TRTC（AppId 以环境变量为准，禁止旧 ID 兜底） |

不使用 MongoDB、MySQL。

朋友圈、用户资料、SMTP 配置、管理日志都在 PostgreSQL。

---

## 本地 / 部署

```bash
cd cqim-app
cp .env.example .env
# DATABASE_URL 必须是 postgresql://...
# 填 MinIO、主库密码与 PUBLIC_BASE_URL
./scripts/deploy.sh
```

详细步骤和冒烟清单见 [cqim-app/docs/DEPLOY.md](./cqim-app/docs/DEPLOY.md)。

生产只使用 `main`。不要合并 C++ / MTProto 实验分支，不要把 `/api` 指到 C++。

---

## 还没做完

- 现网是否已按本文档完成切库与冒烟，以部署机为准
- iOS 与 Web 对齐（另仓）
- 真机杀进程推送验证

## 版权

本仓为私有仓，**不开源**。保留所有权利。
