# node1 单机模式（wed.imim.chat）

当只需 **node1（42.194.167.201）** 独立运行、暂不接入 node2/node3 时使用。

## CDN 关闭 / DNS 直连 node1

DNS 指向 `42.194.167.201` 后，**必须**使用 node1 上的 **Let's Encrypt** 证书（`/etc/letsencrypt/live/wed.imim.chat/`）。  
勿再使用 `/etc/nginx/ssl/` 下的 TrustAsia 单文件证书，否则浏览器会报「无法验证服务器身份」。

`node1-standalone.sh` 会自动切换 Nginx 到 Let's Encrypt。

## 一键切换

在 node1 上，进入代码目录后执行：

```bash
cd /home/ubuntu/cqim-release
sudo bash deploy/scripts/node1-standalone.sh
```

脚本会：

1. 将 Nginx upstream 改为 **仅** `127.0.0.1:3011`（API/页面/私聊 WS）和 `127.0.0.1:8082`（群聊 WS）
2. 确保 `/api/crypto/` 固定走本机 SQLite
3. 重载 Nginx

## 热更新 API + 前端

```bash
npm run build
CONTAINER=cqim-release-api bash deploy/scripts/hot-deploy-api.sh
```

## 与集群模式的区别

| 项目 | 单机 node1 | 三节点集群 |
|------|-----------|-----------|
| Nginx upstream | 仅 127.0.0.1 | node1 + node2 + node3 |
| SQLite | 本机 `/home/ubuntu/cqim_shared/data` | NFS 共享 |
| Redis | 本机 Docker `redis:6379` | node1 6380，从节点远程连接 |
| EdgeOne 源站 | 仅 node1 | 仍仅 node1（入口不变） |

## 恢复多节点

```bash
sudo bash deploy/scripts/node1-add-node3.sh
```
