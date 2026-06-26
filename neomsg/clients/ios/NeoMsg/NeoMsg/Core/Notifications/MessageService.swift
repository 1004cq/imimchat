import Foundation

extension Notification.Name {
    static let neomsgNewMessageReceived = Notification.Name("neomsg.newMessageReceived")
    static let neomsgAPNsTokenUpdated = Notification.Name("neomsg.apnsTokenUpdated")
}

struct IncomingMessagePayload {
    let chatId: Int64
    let messageId: Int64?
    let senderId: Int64?
    let preview: String?
}

/// 新消息统一入口：更新链路广播 + 触觉/提示音
final class MessageService {
    static let shared = MessageService()

    var soundEnabled = true
    var vibrationEnabled = true

    private init() {}

    func onNewMessageReceived(
        _ payload: IncomingMessagePayload,
        playAlert: Bool = true,
        isUrgent: Bool = true
    ) {
        if payload.chatId > 0 {
            DispatchQueue.main.async {
                var userInfo: [String: Any] = ["chatId": payload.chatId]
                if let messageId = payload.messageId { userInfo["messageId"] = messageId }
                if let senderId = payload.senderId { userInfo["senderId"] = senderId }
                if let preview = payload.preview { userInfo["preview"] = preview }
                NotificationCenter.default.post(
                    name: .neomsgNewMessageReceived,
                    object: nil,
                    userInfo: userInfo
                )
            }
        }

        guard playAlert else { return }
        NotificationManager.shared.playNewMessageAlert(
            isUrgent: isUrgent,
            playSound: soundEnabled,
            playVibration: vibrationEnabled
        )
    }
}
