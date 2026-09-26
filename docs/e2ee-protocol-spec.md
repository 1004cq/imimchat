# imim E2EE 协议规范（Web ↔ iOS 互操作）

> 版本：2026-09-26
> 目标：iOS 端实现此规范后，可与 Web 端（`cqim-app/client/src/lib/e2ee/`）互通加解密。

## 1. 密码学原语

| 用途 | 算法 | 参数 |
|------|------|------|
| 密钥交换 | ECDH | P-256 曲线 |
| 签名 | ECDSA | P-256 + SHA-256 |
| 密钥派生 | HKDF | SHA-256，salt = 32 字节全 0 |
| 对称加密 | AES-256-GCM | 12 字节随机 IV，前置于密文 |

**关键约束**：Web Crypto 的 P-256 密钥不能同时用于 ECDH 和 ECDSA。
因此每个用户有两对密钥：
- **Identity Key**（ECDH）：身份标识，用于 X3DH
- **Signing Key**（ECDSA）：专门用于签名 Signed PreKey

iOS 若用同一把密钥做两件事，需确保库支持，或同样生成两对。

## 2. 密钥 Bundle 格式（上传到 `/api/crypto/register-bundle`）

```json
{
  "userId": "xxx",
  "registrationId": 12345,
  "identityKey": "base64(P-256公钥, uncompressed)",
  "signingPublicKey": "base64(P-256公钥, uncompressed)",
  "signedPreKey": {
    "keyId": 1,
    "publicKey": "base64(...)",
    "signature": "base64(ECDSA签名)"
  },
  "preKeys": [
    { "keyId": 1, "publicKey": "base64(...)" },
    ...
  ]
}
```

**签名规则**：`signature = ECDSA_Sign(signingPrivateKey, signedPreKey.publicKey原始字节)`

## 3. X3DH 密钥协商

### 3.1 发起方（Alice → Bob）

从服务器获取 Bob 的 Bundle 后：

```
DH1 = ECDH(Alice_IdentityPriv, Bob_SignedPreKeyPub)
DH2 = ECDH(Alice_EphemeralPriv, Bob_IdentityPub)
DH3 = ECDH(Alice_EphemeralPriv, Bob_SignedPreKeyPub)
DH4 = ECDH(Alice_EphemeralPriv, Bob_OneTimePreKeyPub)  // 若有
DH = DH1 || DH2 || DH3 || [DH4]
rootKey = HKDF(DH, salt=0x00*32, info="imim-x3dh", 32字节)
```

然后做首次发送棘轮：
```
dhSend = ECDH(Alice_RatchetPriv(新生成), Bob_SignedPreKeyPub)
derived = HKDF(rootKey || dhSend, salt=0x00*32, info="imim-chain", 64字节)
newRootKey = derived[0:32]
sendChainKey = derived[32:64]
```

### 3.2 响应方（Bob 收到 PreKey 消息）

```
DH1 = ECDH(Bob_SignedPreKeyPriv, Alice_IdentityPub)
DH2 = ECDH(Bob_IdentityPriv, Alice_EphemeralPub)
DH3 = ECDH(Bob_SignedPreKeyPriv, Alice_EphemeralPub)
DH4 = ECDH(Bob_OneTimePreKeyPriv, Alice_EphemeralPub)  // 若有
DH = DH1 || DH2 || DH3 || [DH4]
rootKey = HKDF(DH, salt=0x00*32, info="imim-x3dh", 32字节)

dhRecv = ECDH(Bob_SignedPreKeyPriv, Alice_RatchetPub)
derived = HKDF(rootKey || dhRecv, salt=0x00*32, info="imim-chain", 64字节)
newRootKey = derived[0:32]
receiveChainKey = derived[32:64]
```

**验证**：双方得到的 `newRootKey` 必须相同，Alice 的 `sendChainKey` 必须等于 Bob 的 `receiveChainKey`。

## 4. Double Ratchet 消息加解密

### 4.1 对称棘轮（每条消息）

```
messageKey, nextChainKey = KDF(chainKey)
// KDF 实现：HMAC-SHA256(chainKey, 0x01) = messageKey
//           HMAC-SHA256(chainKey, 0x02) = nextChainKey
```

### 4.2 DH 棘轮（收到新 ratchet key 时）

当 `envelope.senderRatchetKey != 本地 dhReceivingKey`：

```
// 接收链
dh = ECDH(本地发送私钥, 对端新棘轮公钥)
derived = HKDF(rootKey || dh, salt, info="imim-ratchet", 64)
rootKey = derived[0:32]
receiveChainKey = derived[32:64]

// 发送链（生成新密钥对）
newKP = GenerateKeyPair()
dh2 = ECDH(newKP.priv, 对端新棘轮公钥)
derived2 = HKDF(rootKey || dh2, salt, info="imim-ratchet", 64)
rootKey = derived2[0:32]
sendChainKey = derived2[32:64]
dhSendingKeyPair = newKP
```

### 4.3 消息信封格式

```typescript
{
  type: 'prekey' | 'message',
  senderRegistrationId: number,
  senderIdentityKey: string,      // base64
  senderEphemeralKey?: string,    // 仅 prekey 消息，base64
  usedOneTimePreKeyId?: number,   // 仅 prekey 消息
  usedSignedPreKeyId?: number,    // 仅 prekey 消息
  senderRatchetKey: string,       // base64
  previousCounter: number,
  counter: number,                // 发送计数（从1开始）
  ciphertext: {
    iv: string,                  // base64, 12字节
    data: string                 // base64, AES-GCM密文
  },
  timestamp: number
}
```

## 5. 会话重置

- Web 端点"重试"会删除本地会话，下次发送时重新 X3DH。
- 收到 `type='prekey'` 消息时，若本地有会话且 `receiveCounter==0` 且无 `receiveChainKey`（即我是发起方但还没收到回复），比较双方 identityKey 大小：大的保留发起方角色，小的转为响应方重建会话。

## 6. 已知兼容性问题

| 问题 | 状态 |
|------|------|
| iOS Bundle 缺 `signingPublicKey` | Web 已降级为警告（2026-09-26修复），但 iOS 应尽快补上 |
| 自定义 `imim-*` info 字符串 | 非标准 Signal，必须严格按此文档实现 |

## 7. 调试

Web 端控制台会输出 `[E2EE]` 前缀日志。关键检查点：
- `[E2EE] 开始 X3DH 密钥协商` → 发起方开始
- `[E2EE] 响应方 X3DH 完成` → 响应方完成
- `[E2EE] DH 棘轮步骤完成` → 收到新 ratchet key
- `[E2EE] 消息已加密/解密` → 对称棘轮正常
