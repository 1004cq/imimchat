import UIKit
import AudioToolbox

/// 前台/后台新消息触觉 + 提示音
/// - 震动：`UIImpactFeedbackGenerator`（比 `AudioServicesPlayAlertSound` 更强、更现代）
/// - 提示音：优先 `urgent_message.caf`，否则系统 1005（尖锐急促）
final class NotificationManager {
    static let shared = NotificationManager()

    private let fallbackUrgentSoundID: SystemSoundID = 1005
    private let normalSoundID: SystemSoundID = 1000
    private var customSoundID: SystemSoundID = 0
    private var hasCustomSound = false

    private init() {
        registerCustomSound()
    }

    private func registerCustomSound() {
        let bundles = [Bundle.main]
        for bundle in bundles {
            guard let url = bundle.url(forResource: "urgent_message", withExtension: "caf") else { continue }
            var soundID: SystemSoundID = 0
            let status = AudioServicesCreateSystemSoundID(url as CFURL, &soundID)
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

    func playNewMessageAlert(
        isUrgent: Bool = true,
        playSound: Bool = true,
        playVibration: Bool = true
    ) {
        if playVibration {
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
        playNewMessageAlert(isUrgent: true)
    }
}
