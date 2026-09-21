import Capacitor

@objc(MessageAlertPlugin)
public class MessageAlertPlugin: CAPPlugin, CAPBridgedPlugin {
    public let identifier = "MessageAlertPlugin"
    public let jsName = "MessageAlert"
    public let pluginMethods: [CAPPluginMethod] = [
        CAPPluginMethod(name: "playNewMessageAlert", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "onNewMessageReceived", returnType: CAPPluginReturnPromise),
    ]

    @objc func playNewMessageAlert(_ call: CAPPluginCall) {
        let urgent = call.getBool("urgent") ?? true
        let playSound = call.getBool("sound") ?? true
        let playVibration = call.getBool("vibration") ?? true

        DispatchQueue.main.async {
            MessageService.shared.handleForegroundMessage(
                isUrgent: urgent,
                playSound: playSound,
                playVibration: playVibration
            )
            call.resolve()
        }
    }

    @objc func onNewMessageReceived(_ call: CAPPluginCall) {
        let chatId = call.getString("chatId") ?? ""
        let messageId = call.getString("messageId")
        let senderId = call.getString("senderId")
        let preview = call.getString("preview")
        let urgent = call.getBool("urgent") ?? true
        let playSound = call.getBool("sound") ?? true
        let playVibration = call.getBool("vibration") ?? true
        let playAlert = call.getBool("playAlert") ?? true

        let payload = IncomingMessagePayload(
            chatId: chatId,
            messageId: messageId,
            senderId: senderId,
            preview: preview
        )

        MessageService.shared.onNewMessageReceived(
            payload,
            playAlert: playAlert,
            isUrgent: urgent,
            playSound: playSound,
            playVibration: playVibration
        )
        call.resolve()
    }
}
