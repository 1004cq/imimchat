# 自定义急促提示音

将短促、类似 Telegram「叮」声的音频文件命名为 **`urgent_message.caf`**，任选一种方式加入工程：

## 方式 A（推荐）：拖入 Xcode 主 App

1. `npx cap sync ios` 后用 Xcode 打开 `ios/App/App.xcworkspace`
2. 将 `urgent_message.caf` 拖入 **App** target（勾选 *Copy items if needed*）
3. 确认 **Target Membership** 勾选 `App`
4. 真机运行，前台收消息应播放自定义音（而非系统 1005）

## 方式 B：放入插件 Resources

1. 把文件放到本目录：`ios/Sources/MessageAlertPlugin/Resources/urgent_message.caf`
2. 重新 `npx cap sync ios`

`NotificationManager` 会优先加载插件 Bundle，再回退主 App Bundle。

## 音频格式建议

| 项 | 建议 |
|----|------|
| 格式 | `.caf`（Core Audio Format，iOS 原生支持最好） |
| 时长 | 0.1–0.3 秒，短促 |
| 风格 | 类似 Telegram 发送/接收的清脆「叮」 |

若无自定义文件，自动使用系统音效 **1005**（尖锐急促）。
