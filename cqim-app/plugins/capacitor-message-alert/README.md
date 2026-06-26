# capacitor-message-alert

iOS 前台新消息触觉 + 提示音（Capacitor 8 本地插件）。

## 效果

- **震动**：`UIImpactFeedbackGenerator`（heavy + medium 组合），比 `AudioServicesPlayAlertSound` 更现代
- **提示音**：优先 `urgent_message.caf`，否则系统 **1005**（尖锐急促）

## 自定义音效（推荐）

将 Telegram 风格的短促 `urgent_message.caf` 放入 Xcode App target，或见  
`ios/Sources/MessageAlertPlugin/Resources/README.md`。

## JS API

```ts
import { MessageAlert } from 'capacitor-message-alert';

await MessageAlert.onNewMessageReceived({
  chatId: 'xxx',
  urgent: true,
  sound: true,
  vibration: true,
});
```

应用内已封装为 `playNativeMessageAlert()`（`client/src/lib/notifications.ts`）。
