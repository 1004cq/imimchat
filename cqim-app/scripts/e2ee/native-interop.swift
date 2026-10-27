// Compiled with the production E2EEEnvelope.swift and E2EE_REGRESSION_TESTS.
// No real network, Apple account, Keychain, or user chat data is accessed.
import Foundation
import CryptoKit

final class APIClient {
    static let shared = APIClient()
    var remote: E2EEPreKeyBundle?
    func fetchE2EEPreKeyCount(userId: String) async throws -> Int { 20 }
    func replenishE2EEPreKeys(userId: String, preKeys: [E2EEPreKeyUpload]) async throws {}
    func registerE2EEBundle(userId: String, bundle: E2EELocalPreKeyBundle, preKeys: [E2EEPreKeyUpload]) async throws {}
    func fetchE2EEBundle(userId: String) async throws -> E2EEPreKeyBundle {
        guard let remote else { throw E2EEError.invalidRemoteBundle }; return remote
    }
}

private struct Command: Decodable {
    let op: String
    let user: String
    let peer: String?
    let text: String?
    let bundle: E2EEPreKeyBundle?
}

@main struct Harness {
    static func main() async {
        var managers: [String: E2EEManager] = [:]
        while let line = readLine() {
            do {
                let command = try JSONDecoder().decode(Command.self, from: Data(line.utf8))
                let manager = managers[command.user] ?? E2EEManager.regressionInstance()
                managers[command.user] = manager
                UserDefaults.standard.set(command.user, forKey: "current_user_id")
                if let bundle = command.bundle { APIClient.shared.remote = bundle }
                let result: [String: Any]
                switch command.op {
                case "init":
                    try await manager.synchronizeCurrentUser()
                    let bytes = try JSONEncoder().encode(manager.regressionBundle(userId: command.user))
                    result = ["bundle": try JSONSerialization.jsonObject(with: bytes)]
                case "encrypt":
                    result = ["envelope": try await manager.encryptText(command.text!, peerId: command.peer!)]
                case "encrypt_batch":
                    let values = try await withThrowingTaskGroup(of: String.self) { group in
                        for index in 0..<3 {
                            group.addTask { try await manager.encryptText("burst-\(index)", peerId: command.peer!) }
                        }
                        var values: [String] = []
                        for try await value in group { values.append(value) }
                        return values
                    }
                    result = ["envelopes": values]
                case "decrypt":
                    result = ["text": try await manager.decryptText(command.text!, peerId: command.peer!)]
                case "state":
                    let state = try manager.regressionState(userId: command.user, peerId: command.peer!)
                    // Compare a digest, never expose private ratchet material.
                    result = ["state": SHA256.hash(data: state).map { String(format: "%02x", $0) }.joined()]
                default: throw E2EEError.invalidEnvelope
                }
                emit(result)
            } catch {
                emit(["error": true])
            }
        }
    }
    static func emit(_ result: [String: Any]) {
        let data = try! JSONSerialization.data(withJSONObject: result, options: [.sortedKeys])
        print("RESULT " + String(decoding: data, as: UTF8.self))
        fflush(stdout)
    }
}
