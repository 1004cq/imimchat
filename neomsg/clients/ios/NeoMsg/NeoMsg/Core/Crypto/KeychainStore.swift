import Foundation
import Security

/// Keychain 安全存储：JWT Token、E2EE 私钥
enum KeychainStore {
    private static let service = "com.neomsg.app"

    static func saveToken(_ token: String) {
        save(key: "access_token", data: Data(token.utf8))
    }

    static func loadToken() -> String? {
        guard let data = load(key: "access_token") else { return nil }
        return String(data: data, encoding: .utf8)
    }

    static func deleteToken() {
        delete(key: "access_token")
    }

    static func saveIdentityKey(_ key: Data) {
        save(key: "e2ee_identity_key", data: key)
    }

    static func loadIdentityKey() -> Data? {
        load(key: "e2ee_identity_key")
    }

    // MARK: - Keychain primitives

    private static func save(key: String, data: Data) {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: key,
            kSecValueData as String: data,
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly,
        ]
        SecItemDelete(query as CFDictionary)
        SecItemAdd(query as CFDictionary, nil)
    }

    private static func load(key: String) -> Data? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: key,
            kSecReturnData as String: true,
        ]
        var result: AnyObject?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        guard status == errSecSuccess else { return nil }
        return result as? Data
    }

    private static func delete(key: String) {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: key,
        ]
        SecItemDelete(query as CFDictionary)
    }
}
