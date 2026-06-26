import Foundation

/// pts 增量同步引擎（类 Telegram getDifference）
final class SyncEngine {
    private let store: MessageStore

    init(store: MessageStore) { self.store = store }

    /// 启动时拉取增量更新
    func sync(fromPts: Int64) async {
        // TODO: 发送 GetDifferenceRequest(from_pts: fromPts) 通过 ConnectionManager
        // 收到 UpdatesEvent 后调用 applyUpdates
        _ = fromPts
    }

    /// 处理服务端推送的原始数据
    func handlePush(_ data: Data) async {
        // TODO: Protobuf 解码 Envelope，按类型分发
        _ = data
    }

    func applyUpdates(pts: Int64, updates: [Message]) {
        store.updatePts(pts)
        for msg in updates {
            store.appendMessage(msg)
        }
    }
}
