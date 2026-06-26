import Foundation

/// MTProto-like 线协议帧：[4B 大端长度][32B MTHeader][Protobuf Payload]
struct MTHeader {
    var authKeyID: Int64 = 0
    var sessionID: Int64 = 0
    var userID: Int64 = 0
    var payloadType: UInt8 = 2  // 1=WirePacket, 2=Envelope
    var flags: UInt16 = 0
    var seq: UInt32 = 0

    static let byteLength = 32
    static let magic: UInt32 = 0x4E454F4D  // "NEOM"

    func encode() -> Data {
        var data = Data(count: MTHeader.byteLength)
        data.withUnsafeMutableBytes { raw in
            let p = raw.bindMemory(to: UInt8.self)
            var offset = 0
            writeUInt32(MTHeader.magic, to: p, offset: &offset)
            p[offset] = 1; offset += 1  // version
            p[offset] = payloadType; offset += 1
            writeUInt16(flags, to: p, offset: &offset)
            writeInt64(authKeyID, to: p, offset: &offset)
            writeInt64(sessionID, to: p, offset: &offset)
            writeInt64(userID, to: p, offset: &offset)
            writeUInt32(seq, to: p, offset: &offset)
        }
        return data
    }

    static func decode(_ data: Data) -> MTHeader? {
        guard data.count >= byteLength else { return nil }
        var h = MTHeader()
        data.withUnsafeBytes { raw in
            let p = raw.bindMemory(to: UInt8.self)
            var offset = 0
            _ = readUInt32(from: p, offset: &offset)
            _ = p[offset]; offset += 1
            h.payloadType = p[offset]; offset += 1
            h.flags = readUInt16(from: p, offset: &offset)
            h.authKeyID = readInt64(from: p, offset: &offset)
            h.sessionID = readInt64(from: p, offset: &offset)
            h.userID = readInt64(from: p, offset: &offset)
            h.seq = readUInt32(from: p, offset: &offset)
        }
        return h
    }
}

enum WireFrameCodec {
    static func encode(header: MTHeader, protobufPayload: Data) -> Data {
        let body = header.encode() + protobufPayload
        var frame = Data(count: 4 + body.count)
        let len = UInt32(body.count)
        frame[0] = UInt8((len >> 24) & 0xFF)
        frame[1] = UInt8((len >> 16) & 0xFF)
        frame[2] = UInt8((len >> 8) & 0xFF)
        frame[3] = UInt8(len & 0xFF)
        frame.replaceSubrange(4..<frame.count, with: body)
        return frame
    }

    static func decode(_ frame: Data) -> (MTHeader, Data)? {
        guard frame.count >= 4 + MTHeader.byteLength else { return nil }
        let len = (UInt32(frame[0]) << 24) | (UInt32(frame[1]) << 16)
            | (UInt32(frame[2]) << 8) | UInt32(frame[3])
        let total = Int(len)
        guard frame.count >= 4 + total, total >= MTHeader.byteLength else { return nil }
        let headerData = frame.subdata(in: 4..<(4 + MTHeader.byteLength))
        guard let header = MTHeader.decode(headerData) else { return nil }
        let payload = frame.subdata(in: (4 + MTHeader.byteLength)..<(4 + total))
        return (header, payload)
    }
}

// MARK: - Binary helpers

private func writeUInt32(_ v: UInt32, to p: UnsafeMutablePointer<UInt8>, offset: inout Int) {
    p[offset] = UInt8((v >> 24) & 0xFF); offset += 1
    p[offset] = UInt8((v >> 16) & 0xFF); offset += 1
    p[offset] = UInt8((v >> 8) & 0xFF); offset += 1
    p[offset] = UInt8(v & 0xFF); offset += 1
}

private func writeUInt16(_ v: UInt16, to p: UnsafeMutablePointer<UInt8>, offset: inout Int) {
    p[offset] = UInt8((v >> 8) & 0xFF); offset += 1
    p[offset] = UInt8(v & 0xFF); offset += 1
}

private func writeInt64(_ v: Int64, to p: UnsafeMutablePointer<UInt8>, offset: inout Int) {
    for i in (0..<8).reversed() {
        p[offset] = UInt8((v >> (i * 8)) & 0xFF)
        offset += 1
    }
}

private func readUInt32(from p: UnsafePointer<UInt8>, offset: inout Int) -> UInt32 {
    let v = (UInt32(p[offset]) << 24) | (UInt32(p[offset+1]) << 16)
        | (UInt32(p[offset+2]) << 8) | UInt32(p[offset+3])
    offset += 4
    return v
}

private func readUInt16(from p: UnsafePointer<UInt8>, offset: inout Int) -> UInt16 {
    let v = (UInt16(p[offset]) << 8) | UInt16(p[offset+1])
    offset += 2
    return v
}

private func readInt64(from p: UnsafePointer<UInt8>, offset: inout Int) -> Int64 {
    var v: Int64 = 0
    for i in (0..<8).reversed() {
        v = (v << 8) | Int64(p[offset])
        offset += 1
    }
    return v
}
