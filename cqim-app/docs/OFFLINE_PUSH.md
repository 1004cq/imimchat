# CQIM 离线推送方案

离线消息只使用自建通道：iOS 通过 APNs Token Authentication，Web 通过 Web Push VAPID。发送前先读取 presence；**仅**用户处于 `foreground`（前台在聊）时跳过系统推送。WebSocket / Gateway 长连接仍在不算免推——`background` 或 `offline` 一律照常发 APNs / Web Push。

APNs alert 与 VoIP token 均存放在 PostgreSQL 的 `PushDeviceToken` 表中，分别使用 `kind=alert` 与 `kind=voip`，一个用户可以拥有多个设备。APNs 的 P8 私钥只通过 `APNS_P8_KEY` 环境变量注入，绝不写入数据库。

Web Push 订阅保存在用户的 `webPushSubscription` 字段中。消息密文仍只写入数据库；发送端可附带一次性 `pushPreview`（不落库）供 APNs / Web Push 展示通知正文。标题使用发送者昵称，缺省正文为“发来一条消息”。香港节点若连不上 Apple，会经 Redis `cqim:apns:relay` 让云南节点补发一次。

当 APNs 返回 `BadDeviceToken` 或 `Unregistered` 时，服务端删除对应的 `PushDeviceToken` 行；Web Push 返回 404/410 时清理订阅。系统没有其他推送 SDK 或后备通道。
