import UIKit
import AudioToolbox

/// 前台新消息触觉 + 提示音
/// - 震动：`UIImpactFeedbackGenerator`（比 `AudioServicesPlayAlertSound` 更强、更现代）
/// - 提示音：优先 `urgent_message.caf`，否则系统 1005（尖锐急促）
class NotificationManager {
    static let shared = NotificationManager()

    /// 系统默认急促提示音（尖锐、短促）
    private let fallbackUrgentSoundID: SystemSoundID = 1005
    /// 普通新消息提示音
    private let normalSoundID: SystemSoundID = 1000
    private var customSoundID: SystemSoundID = 0
    private var hasCustomSound = false

    private init() {
        registerCustomSound()
    }

    /// 加载 `urgent_message.caf`（插件 Bundle 或主 App Bundle）
    private func registerCustomSound() {
        let bundles = [Bundle(for: NotificationManager.self), Bundle.main]
        for bundle in bundles {
            guard let soundURL = bundle.url(forResource: "urgent_message", withExtension: "caf") else {
                continue
            }
            var soundID: SystemSoundID = 0
            let status = AudioServicesCreateSystemSoundID(soundURL as CFURL, &soundID)
            if status == kAudioServicesNoError, soundID != 0 {
                customSoundID = soundID
                hasCustomSound = true
                return
            }
        }
    }

    private func playUrgentSound() {
        if hasCustomSound {
            AudioServicesPlaySystemSound(customSoundID)
        } else {
            AudioServicesPlaySystemSound(fallbackUrgentSoundID)
        }
    }

    /// 来新消息时调用：震动 + 急促提示音
    func playNewMessageAlert(isUrgent: Bool = true, playSound: Bool = true, playVibration: Bool = true) {
        if playVibration {
            // UIImpactFeedbackGenerator：Taptic Engine，手感比老式系统震动更清晰
            let heavy = UIImpactFeedbackGenerator(style: .heavy)
            heavy.prepare()
            heavy.impactOccurred()

            DispatchQueue.main.asyncAfter(deadline: .now() + 0.1) {
                let medium = UIImpactFeedbackGenerator(style: .medium)
                medium.prepare()
                medium.impactOccurred()
            }
        }

        guard playSound else { return }

        if isUrgent {
            playUrgentSound()
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) { [weak self] in
                self?.playUrgentSound()
            }
        } else {
            AudioServicesPlaySystemSound(normalSoundID)
        }
    }

    func handleForegroundMessage() {
        MessageService.shared.handleForegroundMessage()
    }
}
