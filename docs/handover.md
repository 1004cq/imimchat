# imimchat 项目移交文档

**中文版** | [English Version](./handover_en.md)

> 最后更新：2026-04-05

## 1. 项目基本信息

| 项目 | 值 |
|------|-----|
| 项目名称 | imimchat |
| 访问地址 | https://wed.imim.chat |
| 管理后台 | https://wed.imim.chat/admin |
| 服务器 | 42.194.167.201（腾讯云） |
| 登录用户 | ubuntu |
| 密钥文件 | Q(6).pem（需妥善保管） |
| GitHub 仓库 | https://github.com/1004cq/imimchat（私有） |

---

## 2. 服务器登录

```bash
# SSH 登录服务器
ssh -i Q\(6\).pem ubuntu@42.194.167.201

# 进入项目目录
cd ~/tsdd
```

---

## 3. 服务管理

### 查看所有容器状态

```bash
docker ps --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'
```

### 启动/停止所有服务

```bash
cd ~/tsdd
docker compose up -d    # 启动
docker compose down     # 停止
docker compose restart  # 重启
```

### 查看服务日志

```bash
# 业务 API 日志
docker logs -f --tail=100 tsdd-tangsengdaodaoserver-1

# WuKongIM 日志
docker logs -f --tail=100 tsdd-wukongim-1

# Web 前端日志
docker logs -f --tail=100 tsdd-tangsengdaodaoweb-1
```

---

## 4. 前端定制文件管理

前端是**预编译的 React 应用**，不可修改源码。所有定制通过注入文件实现：

### 文件位置（容器内）

```
/usr/share/nginx/html/
├── index.html                    ← 入口（已注入脚本引用）
├── static/
│   ├── js/
│   │   ├── main.imim2026v2.js    ← 主应用（不可修改）
│   │   ├── imim_adaptive.js      ← 移动端适配脚本（可修改）★
│   │   └── imim_adaptive.js.bak  ← 备份
│   └── css/
│       ├── main.946ff072.css     ← 主样式（不可修改）
│       └── mobile.css            ← 移动端补充样式（可修改）
```

### 修改前端定制文件

```bash
# 1. 修改本地 Git 仓库中的文件
# frontend/static/js/imim_adaptive.js
# frontend/static/css/mobile.css

# 2. 上传到服务器
scp -i Q\(6\).pem frontend/static/js/imim_adaptive.js ubuntu@42.194.167.201:/tmp/
scp -i Q\(6\).pem frontend/static/css/mobile.css ubuntu@42.194.167.201:/tmp/

# 3. 注入到容器
ssh -i Q\(6\).pem ubuntu@42.194.167.201 "
  docker cp /tmp/imim_adaptive.js tsdd-tangsengdaodaoweb-1:/usr/share/nginx/html/static/js/imim_adaptive.js
  docker cp /tmp/mobile.css tsdd-tangsengdaodaoweb-1:/usr/share/nginx/html/static/css/mobile.css
"

# 4. 提交到 Git
git add frontend/
git commit -m "fix: 更新移动端适配脚本"
git push
```

> **重要：** 每次重启或更新 Web 容器后，需要重新注入自定义文件！

---

## 5. 数据库管理

### 连接数据库

```bash
# 方式一：通过 Docker exec
docker exec -it tsdd-mysql-1 mysql -u root -p im
# 密码：见 ~/tsdd/.env 中的 MYSQL_ROOT_PASSWORD

# 方式二：通过 Adminer Web 界面（需要 SSH 隧道）
ssh -i Q\(6\).pem -L 8306:127.0.0.1:8306 ubuntu@42.194.167.201
# 然后浏览器访问 http://localhost:8306
# 服务器: mysql, 用户名: root, 密码: 见 .env
```

### 手动备份数据库

```bash
ssh -i Q\(6\).pem ubuntu@42.194.167.201 "
  docker exec tsdd-mysql-1 mysqldump -u root -p'密码' im | gzip > /tmp/im_backup_\$(date +%Y%m%d).sql.gz
"
scp -i Q\(6\).pem ubuntu@42.194.167.201:/tmp/im_backup_*.sql.gz ./backups/
```

---

## 6. Nginx 配置管理

```bash
# 查看配置
sudo cat /etc/nginx/sites-enabled/tsdd

# 修改配置
sudo nano /etc/nginx/sites-enabled/tsdd

# 测试配置
sudo nginx -t

# 重载配置（不中断服务）
sudo systemctl reload nginx

# 查看 Nginx 状态
sudo systemctl status nginx
```

---

## 7. SSL 证书

```bash
# 证书位置
/etc/nginx/ssl/wed.imim.chat.crt
/etc/nginx/ssl/wed.imim.chat.key

# 查看证书到期时间
openssl x509 -enddate -noout -in /etc/nginx/ssl/wed.imim.chat.crt
```

> **注意：** 证书到期前需手动更新并重载 Nginx！

---

## 8. 管理后台

- **地址：** https://wed.imim.chat/admin
- **用户名：** superAdmin
- **密码：** 见 `~/tsdd/.env` 中的 `TS_ADMINPWD`

管理后台功能：
- 用户管理（封禁/解封）
- 频道/群组管理
- 消息审核
- 系统配置

---

## 9. 测试账号

| 手机号 | 密码 | 说明 |
|--------|------|------|
| 13900000099 | Test123456 | 测试账号 1 |

> 注册新账号：使用手机号 + 验证码 `123456`（固定验证码，测试用）

---

## 10. 已完成的定制工作

| 工作内容 | 完成日期 | 说明 |
|----------|----------|------|
| 品牌定制（Logo/名称/主题色） | 2026-04-03 | imimchat 品牌 |
| Nginx HTTPS 配置 | 2026-04-03 | SSL 证书已配置 |
| 管理后台路径配置 | 2026-04-03 | /admin 路径 |
| WebSocket 代理配置 | 2026-04-03 | /ws 路径 |
| Adminer 安全限制 | 2026-04-04 | 仅本机访问 |
| 移动端适配脚本 v3 | 2026-04-04 | imim_adaptive.js |
| 登录 username 修复 | 2026-04-04 | XHR 拦截器 |
| 底部导航栏 | 2026-04-04 | 微信风格 TabBar |
| GitHub 仓库备份 | 2026-04-05 | 本仓库 |

---

## 11. 紧急故障处理

### 网站无法访问

```bash
# 1. 检查 Nginx
sudo systemctl status nginx
sudo nginx -t
sudo systemctl restart nginx

# 2. 检查容器
docker ps | grep tsdd

# 3. 重启所有服务
cd ~/tsdd && docker compose restart
```

### 前端界面异常

```bash
# 回滚到备份版本
docker exec tsdd-tangsengdaodaoweb-1 \
  cp /usr/share/nginx/html/static/js/imim_adaptive.js.bak \
     /usr/share/nginx/html/static/js/imim_adaptive.js
```

### 数据库连接失败

```bash
# 检查 MySQL 容器
docker logs tsdd-mysql-1 --tail=50

# 重启 MySQL
docker restart tsdd-mysql-1

# 等待健康检查通过后重启业务服务
sleep 30 && docker restart tsdd-tangsengdaodaoserver-1
```

---

## 12. 联系方式与资源

| 资源 | 地址 |
|------|------|
| WuKongIM 文档 | https://githubim.com |
| WuKongIM GitHub | https://github.com/WuKongIM/WuKongIM |
| TangSengDaoDao 文档 | https://tangsengdaodao.com |
| TangSengDaoDao GitHub | https://github.com/TangSengDaoDao/TangSengDaoDaoServer |
| 本项目 GitHub | https://github.com/1004cq/imimchat |
