import Foundation

/// SQLite 本地消息缓存（离线可读 + 快速列表渲染）
final class MessageStore: ObservableObject {
    @Published private(set) var dialogs: [Dialog] = []
    @Published private(set) var messages: [Int64: [Message]] = [:]  // dialogID -> messages
    private(set) var localPts: Int64 = 0

    private let dbPath: URL

    init() {
        let dir = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask)[0]
        dbPath = dir.appendingPathComponent("neomsg.db")
        loadFromDisk()
    }

    func appendMessage(_ message: Message) {
        var list = messages[message.dialogID] ?? []
        list.append(message)
        messages[message.dialogID] = list.sorted { $0.createdAt < $1.createdAt }
        persist()
    }

    func updateMessageStatus(dialogID: Int64, clientMsgID: String, status: MessageStatus) {
        guard var list = messages[dialogID] else { return }
        if let idx = list.firstIndex(where: { $0.clientMsgID == clientMsgID }) {
            list[idx].status = status
            messages[dialogID] = list
        }
    }

    func setDialogs(_ dialogs: [Dialog]) {
        self.dialogs = dialogs.sorted {
            if $0.isPinned != $1.isPinned { return $0.isPinned }
            return ($0.lastMessageTime ?? .distantPast) > ($1.lastMessageTime ?? .distantPast)
        }
        persist()
    }

    func updatePts(_ pts: Int64) {
        localPts = max(localPts, pts)
        UserDefaults.standard.set(localPts, forKey: "local_pts")
    }

    func messagesFor(dialogID: Int64) -> [Message] {
        messages[dialogID] ?? []
    }

    // MARK: - Persistence (简化：JSON 文件，生产用 GRDB)

    private func loadFromDisk() {
        localPts = Int64(UserDefaults.standard.integer(forKey: "local_pts"))
        guard let data = try? Data(contentsOf: dbPath),
              let saved = try? JSONDecoder().decode(StoreSnapshot.self, from: data) else { return }
        dialogs = saved.dialogs
        messages = saved.messages
    }

    private func persist() {
        let snapshot = StoreSnapshot(dialogs: dialogs, messages: messages)
        if let data = try? JSONEncoder().encode(snapshot) {
            try? data.write(to: dbPath)
        }
    }
}

private struct StoreSnapshot: Codable {
    let dialogs: [Dialog]
    let messages: [Int64: [Message]]
}
