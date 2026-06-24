# imimchat Private Instant Messaging System

[**中文版**](./README.md) | **English Version**

> A private, end-to-end encrypted instant messaging system based on [WuKongIM](https://github.com/WuKongIM/WuKongIM) and [TangSengDaoDao](https://github.com/TangSengDaoDao/TangSengDaoDaoServer), with completed brand customization, mobile adaptation, and security hardening.

**Access URL:** https://wed.imim.chat  
**Server IP:** 42.194.167.201 (Tencent Cloud)  
**Admin Dashboard:** https://wed.imim.chat/admin  
**Deployment:** Docker Compose + Nginx Reverse Proxy

---

## Documentation Navigation

| Document | 中文 | English |
|----------|------|---------|
| System Architecture | [architecture.md](./docs/architecture.md) | [architecture_en.md](./docs/architecture_en.md) |
| Optimization Roadmap | [optimization.md](./docs/optimization.md) | [optimization_en.md](./docs/optimization_en.md) |
| Project Handover | [handover.md](./docs/handover.md) | [handover_en.md](./docs/handover_en.md) |

---

## Directory Structure

```
imimchat/
├── docker/                     # Docker deployment configurations
│   ├── docker-compose.yaml     # Current production configuration (customized)
│   ├── docker-compose.yaml.original  # Original official configuration (backup)
│   └── .env.example            # Environment variables template (passwords excluded)
├── nginx/                      # Nginx reverse proxy configurations
│   └── wed.imim.chat.conf      # Main HTTPS configuration
├── frontend/                   # Frontend custom files (injected into Web container)
│   ├── index.html              # Entry HTML (reference, includes script loading order)
│   ├── manifest.json           # PWA configuration
│   └── static/
│       ├── js/
│       │   └── imim_adaptive.js    # Mobile adaptation script v3 (core customization)
│       └── css/
│           └── mobile.css          # Mobile CSS supplementary styles
├── register-service/           # Secure registration middleware (SMS code, rate limit, password checks)
│   ├── app.py                  # Flask API service
│   ├── Dockerfile              # Registration service container image
│   └── requirements.txt        # Python dependencies
├── scripts/                    # Operation and maintenance scripts
│   ├── deploy-frontend.sh      # Deploy frontend custom files
│   ├── backup-db.sh            # Database backup script
│   └── manage.sh               # Service management (start/stop/logs/update)
├── docs/                       # Project documentation
│   ├── architecture_en.md      # System architecture explanation
│   ├── optimization_en.md      # Optimization plan and roadmap
│   └── handover_en.md          # Project handover document
├── .gitignore
├── README.md                   # Chinese README
└── README_en.md                # English README
```

---

## Quick Start

### Prerequisites

| Component | Version |
|-----------|---------|
| OS | Ubuntu 22.04 LTS |
| Docker | 29.x+ |
| Docker Compose | v2+ |
| Nginx | 1.18+ |
| Memory | 4GB+ Recommended |
| Disk | 40GB+ Recommended |

### Fresh Deployment

```bash
# 1. Clone this repository
git clone https://github.com/1004cq/imimchat.git
cd imimchat

# 2. Configure environment variables
cp docker/.env.example docker/.env
nano docker/.env  # Modify all passwords and IP addresses

# 3. Start all services
cd docker && docker compose up -d

# 4. Configure Nginx
sudo cp nginx/wed.imim.chat.conf /etc/nginx/sites-enabled/your-domain.conf
# Modify domain and SSL certificate paths
sudo nginx -t && sudo systemctl reload nginx

# 5. Inject frontend custom files
bash scripts/deploy-frontend.sh
```

### Daily Operations

```bash
# Check service status
bash scripts/manage.sh status

# View API logs
bash scripts/manage.sh logs tangsengdaodaoserver

# Backup database
bash scripts/backup-db.sh

# Update frontend custom files
bash scripts/deploy-frontend.sh
```

---

## Service Architecture

```
User (HTTPS)
    │
    ▼
Nginx (443/80) ─── SSL Termination ─── wed.imim.chat
    │
    ├── /          → Web Frontend (Docker:82)  [tangsengdaodaoweb]
    ├── /v1/       → Business API (Docker:8090) [tangsengdaodaoserver]
    ├── /register/ → Secure Registration Service (Docker:9091) [register-service]
    ├── /ws        → WebSocket (Docker:5200) [wukongim]
    └── /admin     → Admin Dashboard (Docker:83)  [tangsengdaodaomanager]
         │
         ├── tangsengdaodaoserver:8090 ─── MySQL:3306
         │                             └── Redis:6379
         │                             └── Minio:9000
         └── wukongim:5001/5200
```

---

## Core Customizations

### 1. Brand Customization

- **App Name:** imimchat
- **Theme Color:** `#1677ff` (Blue)
- **Logo:** Replaced with imm brand icon
- **PWA Theme Color:** `#1a2e8a`

### 2. Mobile Adaptation (`imim_adaptive.js` v3)

This is the most critical custom file in the project, solving the following issues:

| Issue | Solution |
|-------|----------|
| Login username format error (`0086xxx` vs `86xxx`) | XHR/fetch interceptor auto-correction |
| Mobile layout misalignment (sidebar overlap) | CSS media queries + DOM restructuring |
| Missing bottom navigation bar | Dynamically injected WeChat-style TabBar |
| No sliding animation on chat page | CSS transform animations |
| Oversized collapse button | CSS hiding + replacement |

### 3. Nginx Customization

- HTTP → HTTPS automatic redirection
- WebSocket persistent connection support (`/ws` path)
- Admin dashboard path prefix `/admin`
- File upload size limit 200MB

---

## Account Information

> **Note:** For real passwords, please check the `docker/.env` file (not committed to Git).

| System | Account | Description |
|--------|---------|-------------|
| Admin Dashboard | superAdmin | Backend administrator |
| Test Account | 13900000099 | Normal user testing |
| MySQL | root | Database management |
| Minio | minio | File service management |

---

## Related Resources

- [WuKongIM Official Documentation](https://githubim.com)
- [TangSengDaoDao Official Documentation](https://tangsengdaodao.com)
- [Docker Compose Documentation](https://docs.docker.com/compose/)
