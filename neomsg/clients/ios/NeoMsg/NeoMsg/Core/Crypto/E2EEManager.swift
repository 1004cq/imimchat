import Foundation
import CryptoKit

/// Secret Chat 端到端加密管理器
/// 实现 X3DH 密钥协商 + Double Ratchet（简化框架，生产需 libsignal）
final class E2EEManager {
    private var sessions: [Int64: E2EESession] = [:]  // dialogID -> session

    struct E2EESession {
        let dialogID: Int64
        let peerUserID: Int64
        var sendingChainKey: SymmetricKey
        var receivingChainKey: SymmetricKey
        var sendCounter: UInt32 = 0
        var recvCounter: UInt32 = 0
    }

    /// 发起 Secret Chat：上传 PreKey Bundle，建立会话
    func initiateSession(dialogID: Int64, peerUserID: Int64, peerBundle: PreKeyBundle) throws -> E2EESession {
        let identityKey = P256.KeyAgreement.PrivateKey()
        KeychainStore.saveIdentityKey(identityKey.rawRepresentation)

        // X3DH 简化：ECDH(identity, peer signed prekey)
        let peerPublicKey = try P256.KeyAgreement.PublicKey(rawRepresentation: peerBundle.signedPreKey)
        let sharedSecret = try identityKey.sharedSecretFromKeyAgreement(with: peerPublicKey)

        let chainKey = SymmetricKey(data: SHA256.hash(data: sharedSecret.withUnsafeBytes { Data($0) }))

        let session = E2EESession(
            dialogID: dialogID,
            peerUserID: peerUserID,
            sendingChainKey: chainKey,
            receivingChainKey: chainKey
        )
        sessions[dialogID] = session
        return session
    }

    /// 加密消息（Double Ratchet 简化版）
    func encrypt(dialogID: Int64, plaintext: Data) throws -> Data {
        guard var session = sessions[dialogID] else {
            throw E2EEError.noSession
        }
        let messageKey = HKDF<SHA256>.deriveKey(
            inputKeyMaterial: session.sendingChainKey,
            info: Data("msg-\(session.sendCounter)".utf8),
            outputByteCount: 32
        )
        session.sendCounter += 1
        sessions[dialogID] = session

        let sealed = try AES.GCM.seal(plaintext, using: messageKey)
        return sealed.combined!
    }

    /// 解密消息
    func decrypt(dialogID: Int64, ciphertext: Data) throws -> Data {
        guard var session = sessions[dialogID] else {
            throw E2EEError.noSession
        }
        let messageKey = HKDF<SHA256>.deriveKey(
            inputKeyMaterial: session.receivingChainKey,
            info: Data("msg-\(session.recvCounter)".utf8),
            outputByteCount: 32
        )
        session.recvCounter += 1
        sessions[dialogID] = session

        let box = try AES.GCM.SealedBox(combined: ciphertext)
        return try AES.GCM.open(box, using: messageKey)
    }
}

struct PreKeyBundle {
    let userID: Int64
    let identityKey: Data
    let signedPreKey: Data
    let signedPreKeyID: Int32
    let signature: Data
    let preKeys: [(Int32, Data)]
}

enum E2EEError: Error {
    case noSession
    case decryptFailed
}
