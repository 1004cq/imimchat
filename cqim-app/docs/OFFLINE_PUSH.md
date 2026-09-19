# CQIM 离线推送方案（自建，不用个推）

私有仓说明。对照 OpenIM 的判断，**不接个推中台**。

iOS 自建 = 服务器 P8 直连 `api.push.apple.com`。
Web 自建 = Web Push（VAPID）。
Android 若以后要国内厂商通道再评；主产品线是 iOS + Web。

## 结论

1. 长连接只保证前台；后台/锁屏/杀进程走 APNs。
2. 有 WebSocket ≠ 免推。
3. `push-notify.ts` **不再调个推**。`getui:` token 打日忽略。
4. `POST /api/presence` 已挂；Web 已上报；iOS 另仓要接同一接口。

## 自建链路

```text
消息落库
  → WS 在线推（网关）
  → shouldSkipApns？仅 foreground 免 APNs
  → APNs P8 → Apple → iPhone
  → Web Push → 浏览器
```

不要：个推、用个推转 APNs、VoIP 发文本、Kafka 推送微服务。

JPush/FCM 代码文件还在，仅当 token 已是那两种时的降级，新设备不要再注册个推 CID。

## 验收

A 把 iOS 切后台，B 发私聊，A 锁屏出系统横幅（文案「加密消息」）。
不要再配 GETUI_APPID / GETUI_MASTER_SECRET。
