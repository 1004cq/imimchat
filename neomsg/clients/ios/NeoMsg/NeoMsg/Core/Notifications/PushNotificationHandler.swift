import Foundation
import UserNotifications
import UIKit

/// APNs 推送处理：前台、后台、点击通知
final class PushNotificationHandler: NSObject, UNUserNotificationCenterDelegate {
    static let shared = PushNotificationHandler()

    func registerForPushNotifications() {
        UNUserNotificationCenter.current().delegate = self
        UIApplication.shared.registerForRemoteNotifications()
    }

    func updateDeviceToken(_ token: Data) {
        let hex = token.map { String(format: "%02x", $0) }.joined()
        UserDefaults.standard.set(hex, forKey: "apns_device_token")
        NotificationCenter.default.post(
            name: .neomsgAPNsTokenUpdated,
            object: nil,
            userInfo: ["token": hex]
        )
    }

    // MARK: - UNUserNotificationCenterDelegate

    /// 前台收到推送：仍播放急促音 + 震动（系统 banner 可选）
    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification,
        withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
    ) {
        let userInfo = notification.request.content.userInfo
        handlePayload(userInfo, source: .foregroundPush)
        completionHandler([.banner, .sound, .badge])
    }

    /// 用户点击通知 / 后台唤醒
    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse,
        withCompletionHandler completionHandler: @escaping () -> Void
    ) {
        handlePayload(response.notification.request.content.userInfo, source: .userAction)
        completionHandler()
    }

    /// Silent push（content-available: 1）后台同步
    func handleRemoteNotification(
        userInfo: [AnyHashable: Any],
        fetchCompletionHandler completionHandler: @escaping (UIBackgroundFetchResult) -> Void
    ) {
        handlePayload(userInfo, source: .backgroundSilent)
        completionHandler(.newData)
    }

    private enum PushSource {
        case foregroundPush, backgroundSilent, userAction
    }

    private func handlePayload(_ userInfo: [AnyHashable: Any], source: PushSource) {
        let chatId = (userInfo["dialog_id"] as? NSNumber)?.int64Value
            ?? Int64(userInfo["chat_id"] as? String ?? "")
        let preview = userInfo["preview"] as? String
            ?? (userInfo["aps"] as? [String: Any])?["alert"] as? String

        if let chatId, chatId > 0 {
            MessageService.shared.onNewMessageReceived(
                IncomingMessagePayload(chatId: chatId, messageId: nil, senderId: nil, preview: preview),
                playAlert: source != .userAction
            )
        } else if source == .foregroundPush {
            NotificationManager.shared.handleForegroundMessage()
        }
    }
}
