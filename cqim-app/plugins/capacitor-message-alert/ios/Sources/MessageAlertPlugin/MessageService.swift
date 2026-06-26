import Foundation

extension Notification.Name {
    /// 原生层新消息事件（可供 AppDelegate / 扩展监听，Web UI 仍由 Capacitor JS 更新）
    static let cqimNewMessageReceived = Notification.Name("cqim.newMessageReceived")
}

struct IncomingMessagePayload {
    let chatId: String
    let messageId: String?
    let senderId: String?
    let preview: String?
}

/// 前台新消息统一入口：广播事件 + 震动/提示音
class MessageService {
    static let shared = MessageService()

    private init() {}

    func onNewMessageReceived(
        _ payload: IncomingMessagePayload,
        playAlert: Bool = true,
        isUrgent: Bool = true,
        playSound: Bool = true,
        playVibration: Bool = true
    ) {
        if !payload.chatId.isEmpty {
            DispatchQueue.main.async {
                var userInfo: [String: Any] = ["chatId": payload.chatId]
                if let messageId = payload.messageId { userInfo["messageId"] = messageId }
                if let senderId = payload.senderId { userInfo["senderId"] = senderId }
                if let preview = payload.preview { userInfo["preview"] = preview }

                NotificationCenter.default.post(
                    name: .cqimNewMessageReceived,
                    object: nil,
                    userInfo: userInfo
                )
            }
        }

        guard playAlert else { return }

        NotificationManager.shared.playNewMessageAlert(
            isUrgent: isUrgent,
            playSound: playSound,
            playVibration: playVibration
        )
    }

    func handleForegroundMessage(
        isUrgent: Bool = true,
        playSound: Bool = true,
        playVibration: Bool = true
    ) {
        guard playSound || playVibration else { return }
        NotificationManager.shared.playNewMessageAlert(
            isUrgent: isUrgent,
            playSound: playSound,
            playVibration: playVibration
        )
    }
}
