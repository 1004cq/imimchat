# Teamgram MTProto 全栈部署

[Teamgram](https://github.com/teamgram/teamgram-server) 是成熟的 Go 语言 MTProto 2.0 服务端，兼容 Teamgram 修改版 Telegram 客户端。

## 前置条件

- Docker 20+
- 8GB+ 内存（含 MySQL、Redis、Kafka、etcd、MinIO）
- 开放端口 **10443**（MTProto 主入口）

## 一键部署

```bash
# 从 imimchat 根目录
chmod +x scripts/deploy-mtproto.sh
./scripts/deploy-mtproto.sh teamgram
```

或手动：

```bash
git clone https://github.com/teamgram/teamgram-server.git
cd teamgram-server

# 1. 依赖栈（MySQL / Redis / etcd / Kafka / MinIO）
docker compose -f docker-compose-env.yaml up -d

# 2. 应用服务
docker compose up -d --build
```

## 客户端配置

1. 下载 [Teamgram 客户端](https://teamgram.net/)
2. 配置自定义服务器 IP 为 `wed.imim.chat` 或内网 IP
3. MTProto 端口：**10443**

## 与 imimchat 集成

| 组件 | 建议 |
|------|------|
| Nginx | `stream` 块转发 10443 → Teamgram |
| CQIM Web | 保留，Web 用户不走 MTProto |
| 用户同步 | 编写脚本将 CQIM 用户导入 Teamgram MySQL |
| Bot | CQIM Bot HTTP API 可继续独立运行 |

## 端口一览

| 端口 | 用途 |
|------|------|
| 10443 | MTProto 主网关 |
| 5222 | 备用 MTProto |
| 8801 | HTTP API（部分版本） |

完整端口列表见 Teamgram 官方 `docker-compose.yaml`。

## 故障排查

```bash
docker compose -f docker-compose-env.yaml ps
docker compose ps
docker compose logs -f teamgram
```

常见问题：

- **依赖未就绪：** 等待 MySQL healthcheck 通过后再启动应用
- **客户端连不上：** 检查防火墙 / Nginx stream 是否放行 10443
- **证书问题：** MTProto 默认不走 TLS（与 Telegram 一致），生产可用 443 + 伪装或 VPN
