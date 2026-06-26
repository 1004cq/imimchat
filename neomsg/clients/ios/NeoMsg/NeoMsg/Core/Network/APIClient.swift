import Foundation

/// REST API 客户端
final class APIClient {
    private let baseURL: URL
    private var token: String?
    private let session = URLSession.shared
    private let decoder = JSONDecoder()

    init(baseURL: URL) { self.baseURL = baseURL }

    func setToken(_ token: String) { self.token = token }

    func login(phone: String, password: String) async throws -> LoginResponse {
        try await post("/v1/auth/login", body: [
            "phone": phone,
            "password": password,
            "device_id": DeviceInfo.id,
            "platform": "ios",
        ])
    }

    func register(username: String, phone: String, password: String) async throws {
        let _: [String: Bool] = try await post("/v1/auth/register", body: [
            "username": username,
            "phone": phone,
            "password": password,
        ])
    }

    func listDevices() async throws -> [DeviceInfo_] {
        try await get("/v1/auth/devices")
    }

    // MARK: - HTTP helpers

    private func get<T: Decodable>(_ path: String) async throws -> T {
        var request = URLRequest(url: baseURL.appendingPathComponent(path))
        if let token { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        let (data, response) = try await session.data(for: request)
        try validate(response)
        return try decoder.decode(T.self, from: data)
    }

    private func post<T: Decodable>(_ path: String, body: [String: Any]) async throws -> T {
        var request = URLRequest(url: baseURL.appendingPathComponent(path))
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        if let token { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        request.httpBody = try JSONSerialization.data(withJSONObject: body)
        let (data, response) = try await session.data(for: request)
        try validate(response)
        return try decoder.decode(T.self, from: data)
    }

    private func validate(_ response: URLResponse) throws {
        guard let http = response as? HTTPURLResponse, (200...299).contains(http.statusCode) else {
            throw APIError.serverError
        }
    }
}

enum APIError: Error {
    case serverError
    case decodingError
}

struct DeviceInfo_: Codable, Identifiable {
    let deviceID: String
    let platform: String
    let model: String?
    let lastActive: Date?
    let isCurrent: Bool

    var id: String { deviceID }

    enum CodingKeys: String, CodingKey {
        case deviceID = "device_id"
        case platform, model
        case lastActive = "last_active"
        case isCurrent = "is_current"
    }
}
