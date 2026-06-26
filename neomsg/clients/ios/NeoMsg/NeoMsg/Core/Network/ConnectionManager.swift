import Foundation
import Combine

enum ConnectionState: Equatable {
    case disconnected
    case connecting
    case connected
    case reconnecting
}

/// WebSocket 长连接管理：自动重连、心跳、离线消息队列
final class ConnectionManager: ObservableObject {
    @Published private(set) var state: ConnectionState = .disconnected

    private let wsURL: URL
    private let store: MessageStore
    private let syncEngine: SyncEngine
    private var webSocket: URLSessionWebSocketTask?
    private var heartbeatTimer: Timer?
    private var reconnectAttempt = 0
    private let maxReconnect = 10
    private var pendingQueue: [Data] = []
    private var token: String?
    private var deviceID: String?

    init(wsURL: URL, store: MessageStore, syncEngine: SyncEngine) {
        self.wsURL = wsURL
        self.store = store
        self.syncEngine = syncEngine
    }

    func connect(token: String, deviceID: String) async {
        self.token = token
        self.deviceID = deviceID
        state = .connecting

        var components = URLComponents(url: wsURL, resolvingAgainstBaseURL: false)!
        components.queryItems = [
            URLQueryItem(name: "token", value: token),
            URLQueryItem(name: "device_id", value: deviceID),
        ]

        let session = URLSession(configuration: .default)
        webSocket = session.webSocketTask(with: components.url!)
        webSocket?.resume()

        state = .connected
        reconnectAttempt = 0
        startHeartbeat()
        flushPendingQueue()
        receiveLoop()
    }

    func disconnect() {
        heartbeatTimer?.invalidate()
        webSocket?.cancel(with: .goingAway, reason: nil)
        webSocket = nil
        state = .disconnected
    }

    func send(_ data: Data) {
        guard state == .connected, let ws = webSocket else {
            pendingQueue.append(data)
            return
        }
        ws.send(.data(data)) { [weak self] error in
            if error != nil { self?.scheduleReconnect() }
        }
    }

    // MARK: - Private

    private func receiveLoop() {
        webSocket?.receive { [weak self] result in
            guard let self else { return }
            switch result {
            case .success(let message):
                if case .data(let data) = message {
                    self.handleIncoming(data)
                }
                self.receiveLoop()
            case .failure:
                self.scheduleReconnect()
            }
        }
    }

    private func handleIncoming(_ data: Data) {
        // TODO: Protobuf 解码 Envelope，分发到 SyncEngine / MessageStore
        Task { await syncEngine.handlePush(data) }
    }

    private func startHeartbeat() {
        heartbeatTimer = Timer.scheduledTimer(withTimeInterval: 30, repeats: true) { [weak self] _ in
            // TODO: 发送 PingRequest protobuf
            _ = self
        }
    }

    private func flushPendingQueue() {
        let queue = pendingQueue
        pendingQueue.removeAll()
        queue.forEach { send($0) }
    }

    private func scheduleReconnect() {
        guard reconnectAttempt < maxReconnect,
              let token, let deviceID else { return }
        state = .reconnecting
        reconnectAttempt += 1
        let delay = min(pow(2.0, Double(reconnectAttempt)), 30.0)
        DispatchQueue.main.asyncAfter(deadline: .now() + delay) { [weak self] in
            Task { await self?.connect(token: token, deviceID: deviceID) }
        }
    }
}
