# imimchat 即时通讯系统

[English Version](./README_en.md) | **中文版**

> 融合两套 IM 系统的统一项目：基于 [WuKongIM](https://github.com/WuKongIM/WuKongIM) 的私有化部署方案 + 自研 [CQIM](./cqim-app/) 全栈 IM 应用，统一在 Docker Compose 下编排管理。

**访问地址：** https://wed.imim.chat  
**服务器：** 42.194.167.201（腾讯云）  
**管理后台：** https://wed.imim.chat/admin  
**部署方式：** Docker Compose + Nginx 反向代理

---

## 项目组成

本项目包含两套 IM 系统，可根据需求选择启用：

| 系统 | 技术栈 | 说明 |
|------|--------|------|
| **WuKongIM** | Docker 容器 (WuKongIM + TangSengDaoDao) | 私有化 IM 引擎，提供基础聊天能力 |
| **CQIM** | TypeScript/React + Express + SQLite/MongoDB | 自研全栈 IM，类 TG 能力（频道/Bot/搜索/风控） |

---

## 文档导航

| 文档 | 说明 |
|------|------|
| [architecture.md](./docs/architecture.md) | 系统架构说明 |
| [architecture-tg.md](./docs/architecture-tg.md) | 类 TG 架构图与落地对照 |
| [architecture-tg.html](./docs/architecture-tg.html) | 类 TG 架构可视化图 |
| [optimization.md](./docs/optimization.md) | 优化方案与路线图 |
| [handover.md](./docs/handover.md) | 项目移交文档 |
| [register-service.md](./docs/register-service.md) | 注册中间件说明 |
| [cqim-deploy.md](./docs/cqim-deploy.md) | CQIM 部署指南 |
| [cqim-dev-roadmap.md](./docs/cqim-dev-roadmap.md) | CQIM 开发路线图 |
| [UPGRADE.md](./docs/UPGRADE.md) | **v2.0 升级指南（Bot/搜索/风控）** |
| [MTPROTO_UPGRADE.md](./docs/MTPROTO_UPGRADE.md) | **MTProto 2.0 升级指南（类 Telegram 协议）** |
| [MTProto_CLIENT.md](./neomsg/docs/MTProto_CLIENT.md) | **客户端接入（JWT / Wire / MTProto 示例）** |
| [neomsg 技术方案](./neomsg/docs/ARCHITECTURE.md) | NeoMsg 下一代架构（Go + Protobuf + MTProto） |

---

## 目录结构

```
imimchat/
├── docker/                     # Docker 部署配置
│   ├── docker-compose.yaml     # 统一编排（WuKongIM + CQIM 全部服务）
│   ├── docker-compose.yaml.original  # 原始官方配置（备份）
│   └── .env.example            # 环境变量模板
├── cqim-app/                   # CQIM 自研全栈 IM 应用
│   ├── server/                 # Express 后端（20+ 模块）
│   ├── client/                 # React + Vite 前端
│   ├── prisma/                 # 数据库 Schema
│   ├── go-gateway/             # Go 群聊 WebSocket 网关
│   ├── Dockerfile              # 多阶段构建
│   └── package.json
├── register-service/           # 注册中间件（Flask）
│   └── app/                    # 工厂模式模块化结构
├── nginx/                      # Nginx 反向代理配置
│   └── wed.imim.chat.conf      # 双系统路由规则
├── frontend/                   # WuKongIM 前端定制文件
│   ├── index.html
│   ├── manifest.json
│   └── static/
│       ├── js/
│       │   ├── imim_adaptive.js    # 移动端适配脚本
│       │   └── imim_trtc.js        # TRTC 视频通话模块
│       └── css/
│           └── mobile.css
├── scripts/                    # 运维脚本
│   ├── deploy-frontend.sh
│   ├── backup-db.sh
│   └── manage.sh
├── docs/                       # 项目文档
├── .gitignore
├── README.md
└── README_en.md
```

---

## 快速开始

### 环境要求

| 组件 | 版本 |
|------|------|
| 操作系统 | Ubuntu 22.04 LTS |
| Docker | 29.x+ |
| Docker Compose | v2+ |
| Nginx | 1.18+ |
| 内存 | 8GB+ 推荐（双系统运行） |
| 磁盘 | 80GB+ 推荐 |

### 全新部署

```bash
# 1. 克隆本仓库
git clone https://github.com/1004cq/imimchat.git
cd imimchat

# 2. 配置环境变量
cp docker/.env.example docker/.env
nano docker/.env  # 修改所有密码和 IP 地址

# 3. 启动所有服务
cd docker && docker compose up -d

# 4. 配置 Nginx
sudo cp nginx/wed.imim.chat.conf /etc/nginx/sites-enabled/your-domain.conf
# 修改域名和 SSL 证书路径
sudo nginx -t && sudo systemctl reload nginx

# 5. 注入前端自定义文件
bash scripts/deploy-frontend.sh
```

### 日常运维

```bash
# 查看服务状态
bash scripts/manage.sh status

# CQIM 架构升级（Bot/搜索/风控）
bash scripts/upgrade-cqim.sh

# 查看 API 日志
bash scripts/manage.sh logs tangsengdaodaoserver

# 备份数据库
bash scripts/backup-db.sh

# 更新前端定制文件
bash scripts/deploy-frontend.sh
```

---

## 服务架构

```
用户 (HTTPS)
    │
    ▼
Nginx (443/80) ─── SSL 终止 ─── wed.imim.chat
    │
    ├── /          → WuKongIM Web 前端 (Docker:82)
    ├── /cqim      → CQIM 全栈应用 (Docker:3000)
    ├── /v1/       → 业务 API (Docker:8090)
    ├── /ws        → WuKongIM WebSocket (Docker:5200)
    ├── /ws/group  → CQIM 群聊网关 (Docker:8081)
    ├── /admin     → 管理后台 (Docker:83)
    ├── /register/ → 注册服务 (Docker:9091)
    └── /trtc/     → TRTC UserSig (Docker:9091)
         │
         ├── tangsengdaodaoserver:8090 ─── MySQL:3306
         │                             └── Redis:6379
         │                             └── Minio:9000
         ├── wukongim:5001/5200
         └── cqim:3000 ─── MongoDB:27017
                       └── MySQL:3306 (审计)
                       └── Redis:6379 (缓存)
```

### 数据库说明

| 数据库 | 用途 | 端口 |
|--------|------|------|
| MySQL (`im`) | WuKongIM/TangSengDaoDao 用户数据 | 3306 |
| MySQL (`cqim_audit`) | CQIM 审计与运营日志 | 3306 |
| MongoDB (`cqim`) | CQIM 消息/群组/动态 | 27017 |
| Redis | 缓存/在线状态/会话 | 6379 |

---

## 核心定制说明

### 1. 双系统共存

Nginx 通过路径前缀实现两套系统共存：
- `/` → WuKongIM（基础 IM，轻量级）
- `/cqim` → CQIM 全栈应用（朋友圈、贴纸、AI 等高级功能）

如需将 CQIM 设为主应用，交换 nginx 配置中 `/` 和 `/cqim` 的 proxy_pass 目标即可。

### 2. CQIM 功能亮点

- 朋友圈（Moments）系统
- 贴纸商店与 Lottie 动画贴纸
- AI 对话（OpenAI 集成）
- 阅后即焚消息
- 位置分享（腾讯地图）
- 多渠道推送（个推/FCM/APNs）
- TRTC 视频通话
- Go 网关群聊 WebSocket 加速
- **类 TG 架构扩展**：频道、Bot 平台、全局搜索、风控服务、超级群慢速模式

### 3. WuKongIM 定制

- 移动端适配（底部导航栏、滑动动画）
- TRTC 视频通话前端模块
- 安全注册中间件（短信验证码）
- Nginx 安全加固（CSP、HSTS、速率限制）

---

## 账号信息

> **注意：** 真实密码请查阅 `docker/.env` 文件（不提交到 Git）

| 系统 | 账号 | 说明 |
|------|------|------|
| 管理后台 | superAdmin | 后台管理员 |
| 测试账号 | 13900000099 | 普通用户测试 |
| MySQL | root | 数据库管理 |
| Minio | minio | 文件服务管理 |

---

## 相关资源

- [WuKongIM 官方文档](https://githubim.com)
- [TangSengDaoDao 官方文档](https://tangsengdaodao.com)
- [CQIM 源码](./cqim-app/)
- [TRTC 控制台](https://console.cloud.tencent.com/trtc)
- [Docker Compose 文档](https://docs.docker.com/compose/)
