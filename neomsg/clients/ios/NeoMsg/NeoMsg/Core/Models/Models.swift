import Foundation

// MARK: - 核心数据模型

struct User: Identifiable, Codable, Hashable {
    let id: Int64
    var username: String
    var nickname: String
    var avatarURL: String?
    var bio: String?
    var isBot: Bool = false
    var lastSeen: Date?
}

enum DialogType: String, Codable {
    case privateChat = "private"
    case group
    case supergroup
    case channel
    case secret
}

struct Dialog: Identifiable, Codable, Hashable {
    let id: Int64
    var type: DialogType
    var title: String
    var avatarURL: String?
    var lastMessagePreview: String?
    var lastMessageTime: Date?
    var unreadCount: Int = 0
    var isPinned: Bool = false
    var isMuted: Bool = false
    var isSecret: Bool = false
}

enum MessageType: Int, Codable {
    case text = 1, image, video, audio, file, sticker, location, contact, system, encrypted
}

enum MessageStatus: String, Codable {
    case sending, sent, delivered, read, failed
}

struct Message: Identifiable, Codable, Hashable {
    let id: Int64
    let dialogID: Int64
    let senderID: Int64
    var type: MessageType
    var content: String
    var status: MessageStatus
    var clientMsgID: String?
    var replyToID: Int64?
    var mediaRef: MediaRef?
    var ttlSeconds: Int = 0
    var createdAt: Date
    var isOutgoing: Bool = false

    var isExpired: Bool {
        guard ttlSeconds > 0 else { return false }
        return createdAt.addingTimeInterval(TimeInterval(ttlSeconds)) < Date()
    }
}

struct MediaRef: Codable, Hashable {
    let fileID: String
    var mimeType: String
    var size: Int64
    var width: Int?
    var height: Int?
    var duration: Int?
    var thumbnailURL: String?
}

struct LoginResponse: Codable {
    let accessToken: String
    let refreshToken: String
    let expiresIn: Int64
    let userID: Int64

    enum CodingKeys: String, CodingKey {
        case accessToken = "access_token"
        case refreshToken = "refresh_token"
        case expiresIn = "expires_in"
        case userID = "user_id"
    }
}
