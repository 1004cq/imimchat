import Capacitor

@objc(MessageAlertPlugin)
public class MessageAlertPlugin: CAPPlugin, CAPBridgedPlugin {
    public let identifier = "MessageAlertPlugin"
    public let jsName = "MessageAlert"
    public let pluginMethods: [CAPPluginMethod] = [
        CAPPluginMethod(name: "playNewMessageAlert", returnType: CAPPluginReturnPromise),
    ]

    @objc func playNewMessageAlert(_ call: CAPPluginCall) {
        let urgent = call.getBool("urgent") ?? true
        let playSound = call.getBool("sound") ?? true
        let playVibration = call.getBool("vibration") ?? true

        DispatchQueue.main.async {
            NotificationManager.shared.playNewMessageAlert(
                isUrgent: urgent,
                playSound: playSound,
                playVibration: playVibration
            )
            call.resolve()
        }
    }
}
