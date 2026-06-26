# imimchat 项目升级指南

> 版本：v2.0（类 TG 架构升级）· 2026-06-26

本文档说明如何将现有 imimchat 部署升级到包含 **频道、Bot 平台、全局搜索、风控、超级群慢速模式** 的新版本。

---

## 升级内容一览

| 模块 | 新增能力 | 路径 |
|------|---------|------|
| 频道服务 | 单向广播、公开订阅 | `/api/channel/*` |
| Bot 平台 | 类 TG Bot API + OneBot 兼容 | `/api/bot/*` · `/bot{token}/*` |
| 搜索服务 | 消息/用户/群/频道/文件 | `/api/search` |
| 风控服务 | 敏感词、限流、慢速模式 | `/api/risk/*` |
| 消息队列 | Redis Pub/Sub 异步事件 | 内部 `server/mq.ts` |
| 超级群 | 慢速模式、权限设置 | `PUT /api/group/settings` |
| 管理后台 | Bot 平台、风控配置面板 | 管理后台新菜单 |
| 前端 | 全局搜索页 | 消息列表 → 搜索栏 |

长期演进方向见 [neomsg/docs/ARCHITECTURE.md](../neomsg/docs/ARCHITECTURE.md)（Go + Protobuf 下一代架构）。

---

## 升级步骤

### 1. 拉取代码

```bash
cd imimchat
git fetch origin
git checkout cursor/imimchat-upgrade-7467   # 或合并后的 master
```

### 2. 同步数据库 Schema

CQIM 新增了 `BotToken`、`BotUpdate` 表及 `Group.slowModeSeconds` 字段：

```bash
cd cqim-app
pnpm install
pnpm db:push
```

### 3. 重建并重启 CQIM 容器

```bash
cd docker
docker compose build cqim
docker compose up -d cqim
```

### 4. 验证服务

```bash
# 健康检查
curl -s http://localhost:3000/api/health

# 风控配置（管理后台 API）
curl -s -H "Authorization: Bearer <admin_token>" http://localhost:3000/api/admin/risk-config

# Bot 列表
curl -s -H "Authorization: Bearer <admin_token>" http://localhost:3000/api/admin/bots
```

### 5. 前端验证

访问 `https://wed.imim.chat/cqim`（或你的 CQIM 路径）：

- 消息列表点击搜索栏 → 全局搜索
- 管理后台 → **Bot 平台** / **风控配置** 新菜单

---

## 一键升级脚本

```bash
bash scripts/upgrade-cqim.sh
```

---

## 新增 API 速查

### 搜索（需用户登录 Token）

```
GET /api/search?q=关键词&scope=all|messages|users|groups|channels|files
Authorization: Bearer <user_token>
```

### 创建 Bot（需用户登录）

```
POST /api/bot/create
{ "ownerId": "<userId>", "name": "MyBot", "username": "my_bot" }
```

### Bot API（Telegram 风格）

```
GET  /bot<token>/getMe
POST /bot<token>/sendMessage  { "chat_id": "...", "text": "hello" }
POST /bot<token>/setWebhook   { "url": "https://..." }
GET  /bot<token>/getUpdates?offset=0&timeout=30
```

### 超级群慢速模式（仅群主）

```
PUT /api/group/settings
{ "groupId": "...", "userId": "...", "slowModeSeconds": 30 }
```

---

## 双系统说明

升级后 imimchat 仍保持双系统共存：

| 路径 | 系统 | 说明 |
|------|------|------|
| `/` | WuKongIM + TangSengDaoDao | 基础 IM |
| `/cqim` | CQIM 自研全栈 | **本次升级主体** |
| `/admin` | 管理后台 | WuKongIM 后台 |
| CQIM 内置管理 | `/cqim` 内 AdminPage | 含新风控/Bot 面板 |

如需将 CQIM 设为主应用，交换 nginx 中 `/` 与 `/cqim` 的 `proxy_pass` 目标。

---

## 回滚

```bash
cd docker
docker compose down cqim
git checkout master
docker compose build cqim && docker compose up -d cqim
```

数据库新增字段为向后兼容，回滚代码不影响已有数据。

---

## 后续规划

- **短期**：Elasticsearch 全文索引、Bot 管理前台页面
- **中期**：Android 客户端、频道推荐
- **长期**：NeoMsg（Go + Protobuf）逐步替换 CQIM 后端
