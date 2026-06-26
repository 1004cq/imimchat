import Foundation
import Combine

/// 应用级协调器：管理认证状态、连接生命周期
@MainActor
final class AppCoordinator: ObservableObject {
    @Published var isAuthenticated = false
    @Published var currentUser: User?
    @Published var connectionState: ConnectionState = .disconnected

    let connectionManager: ConnectionManager
    let messageStore: MessageStore
    let syncEngine: SyncEngine
    let apiClient: APIClient

    private var cancellables = Set<AnyCancellable>()

    init() {
        self.apiClient = APIClient(baseURL: AppConfig.apiBaseURL)
        self.messageStore = MessageStore()
        self.syncEngine = SyncEngine(store: messageStore)
        self.connectionManager = ConnectionManager(
            wsURL: AppConfig.wsBaseURL,
            store: messageStore,
            syncEngine: syncEngine
        )

        connectionManager.$state
            .receive(on: DispatchQueue.main)
            .assign(to: &$connectionState)

        NotificationCenter.default.publisher(for: .neomsgAPNsTokenUpdated)
            .compactMap { $0.userInfo?["token"] as? String }
            .sink { [weak self] token in
                Task { await self?.uploadPushToken(token) }
            }
            .store(in: &cancellables)
    }

    private func uploadPushToken(_ token: String) async {
        try? await apiClient.registerPushToken(token, deviceID: DeviceInfo.id)
    }

    func bootstrap() async {
        if let token = KeychainStore.loadToken() {
            apiClient.setToken(token)
            isAuthenticated = true
            await connectionManager.connect(token: token, deviceID: DeviceInfo.id)
            await syncEngine.sync(fromPts: messageStore.localPts)
        }
    }

    func login(phone: String, password: String) async throws {
        let response = try await apiClient.login(phone: phone, password: password)
        KeychainStore.saveToken(response.accessToken)
        apiClient.setToken(response.accessToken)
        currentUser = User(id: response.userID, username: phone, nickname: phone)
        isAuthenticated = true
        await connectionManager.connect(token: response.accessToken, deviceID: DeviceInfo.id)
    }

    func logout() {
        connectionManager.disconnect()
        KeychainStore.deleteToken()
        isAuthenticated = false
        currentUser = nil
    }
}

enum AppConfig {
    static let apiBaseURL = URL(string: "http://localhost:8090")!
    static let wsBaseURL = URL(string: "ws://localhost:8080/ws")!
}

enum DeviceInfo {
    static let id: String = {
        if let id = UserDefaults.standard.string(forKey: "device_id") { return id }
        let id = UUID().uuidString
        UserDefaults.standard.set(id, forKey: "device_id")
        return id
    }()
}
