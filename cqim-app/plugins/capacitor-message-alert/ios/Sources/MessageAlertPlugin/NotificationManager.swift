import UIKit
import AudioToolbox

class NotificationManager {
    static let shared = NotificationManager()

    private let urgentSoundID: SystemSoundID = 1005
    private var customSoundID: SystemSoundID = 0

    private init() {}

    private func registerCustomSound() {
        if let soundURL = Bundle.main.url(forResource: "urgent_message", withExtension: "caf") {
            AudioServicesCreateSystemSoundID(soundURL as CFURL, &customSoundID)
        }
    }

    /// 来新消息时调用：震动 + 急促提示音
    func playNewMessageAlert(isUrgent: Bool = true, playSound: Bool = true, playVibration: Bool = true) {
        if playVibration {
            let generator = UIImpactFeedbackGenerator(style: .heavy)
            generator.prepare()
            generator.impactOccurred()

            DispatchQueue.main.asyncAfter(deadline: .now() + 0.1) {
                let followUp = UIImpactFeedbackGenerator(style: .medium)
                followUp.prepare()
                followUp.impactOccurred()
            }
        }

        guard playSound else { return }

        if isUrgent {
            AudioServicesPlaySystemSound(urgentSoundID)
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) {
                AudioServicesPlaySystemSound(self.urgentSoundID)
            }
        } else {
            AudioServicesPlaySystemSound(1000)
        }
    }

    func handleForegroundMessage() {
        MessageService.shared.handleForegroundMessage()
    }
}
