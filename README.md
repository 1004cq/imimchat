# imimchat 私有化即时通讯系统

[English Version](./README_en.md) | **中文版**

> 基于 [WuKongIM](https://github.com/WuKongIM/WuKongIM) + [TangSengDaoDao](https://github.com/TangSengDaoDao/TangSengDaoDaoServer) 搭建的端到端加密私有化即时通讯系统，已完成品牌定制、移动端适配和安全加固。

**访问地址：** https://wed.imim.chat  
**服务器：** 42.194.167.201（腾讯云）  
**管理后台：** https://wed.imim.chat/admin  
**部署方式：** Docker Compose + Nginx 反向代理

---

## 文档导航

| 文档 | 中文 | English |
|------|------|---------|
| 系统架构说明 | [architecture.md](./docs/architecture.md) | [architecture_en.md](./docs/architecture_en.md) |
| 优化方案路线图 | [optimization.md](./docs/optimization.md) | [optimization_en.md](./docs/optimization_en.md) |
| 项目移交手册 | [handover.md](./docs/handover.md) | [handover_en.md](./docs/handover_en.md) |

---

## 目录结构

```
imimchat/
├── docker/                     # Docker 部署配置
│   ├── docker-compose.yaml     # 当前生产配置（已定制）
│   ├── docker-compose.yaml.original  # 原始官方配置（备份）
│   └── .env.example            # 环境变量模板（不含真实密码）
├── nginx/                      # Nginx 反向代理配置
│   └── wed.imim.chat.conf      # 主站 HTTPS 配置
├── frontend/                   # 前端自定义文件（注入到 Web 容器）
│   ├── index.html              # 入口 HTML（参考，含脚本加载顺序）
│   ├── manifest.json           # PWA 配置
│   └── static/
│       ├── js/
│       │   └── imim_adaptive.js    # 移动端适配脚本 v3（核心定制）
│       └── css/
│           └── mobile.css          # 移动端 CSS 补充样式
├── scripts/                    # 运维脚本
│   ├── deploy-frontend.sh      # 部署前端自定义文件
│   ├── backup-db.sh            # 数据库备份
│   └── manage.sh               # 服务管理（启停/日志/更新）
├── docs/                       # 项目文档（中英双语）
│   ├── architecture.md         # 系统架构说明（中文）
│   ├── architecture_en.md      # System Architecture (English)
│   ├── optimization.md         # 优化方案与路线图（中文）
│   ├── optimization_en.md      # Optimization Plan & Roadmap (English)
│   ├── handover.md             # 项目移交文档（中文）
│   └── handover_en.md          # Project Handover Document (English)
├── .gitignore
├── README.md                   # 中文说明
└── README_en.md                # English README
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
| 内存 | 4GB+ 推荐 |
| 磁盘 | 40GB+ 推荐 |

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
    ├── /          → Web 前端 (Docker:82)  [tangsengdaodaoweb]
    ├── /v1/       → 业务 API (Docker:8090) [tangsengdaodaoserver]
    ├── /ws        → WebSocket (Docker:5200) [wukongim]
    └── /admin     → 管理后台 (Docker:83)  [tangsengdaodaomanager]
         │
         ├── tangsengdaodaoserver:8090 ─── MySQL:3306
         │                             └── Redis:6379
         │                             └── Minio:9000
         └── wukongim:5001/5200
```

---

## 核心定制说明

### 1. 品牌定制

- **应用名称：** imimchat
- **主题色：** `#1677ff`（蓝色）
- **Logo：** 已替换为 imm 品牌图标
- **PWA 主题色：** `#1a2e8a`

### 2. 移动端适配（`imim_adaptive.js` v3）

这是本项目最核心的定制文件，解决了以下问题：

| 问题 | 解决方案 |
|------|----------|
| 登录 username 格式错误（`0086xxx` vs `86xxx`） | XHR/fetch 拦截器自动修正 |
| 移动端布局错乱（侧边栏遮挡） | CSS 媒体查询 + DOM 重构 |
| 缺少底部导航栏 | 动态注入微信风格 TabBar |
| 聊天页面无滑动动画 | CSS transform 动画 |
| 折叠按钮过大 | CSS 隐藏 + 替换 |

### 3. Nginx 定制

- HTTP → HTTPS 自动重定向
- WebSocket 长连接支持（`/ws` 路径）
- 管理后台路径前缀 `/admin`
- 文件上传大小限制 200MB

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
- [Docker Compose 文档](https://docs.docker.com/compose/)
