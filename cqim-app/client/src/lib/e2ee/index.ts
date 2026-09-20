/**
 * imim E2EE 模块入口
 * 基于 Signal Protocol 的端到端加密实现
 */

export { SignalStore } from './SignalStore';
export type {
  KeyPairB64,
  IdentityRecord,
  SignedPreKeyRecord,
  PreKeyRecord,
  SessionRecord,
  LocalRegistration,
} from './SignalStore';

export {
  bufferToBase64,
  base64ToBuffer,
  bufferToHex,
  hexToBuffer,
  stringToBuffer,
  bufferToString,
  concatBuffers,
  randomBytes,
  generateRegistrationId,
  generateKeyPair,
  exportKeyPair,
  importPublicKey,
  importPrivateKey,
  ecdh,
  generateSigningKeyPair,
  sign,
  verify,
  signSignedPreKey,
  verifySignedPreKeySignature,
  assertValidSignedPreKey,
  SIGNED_PREKEY_MISSING_SIGNING_KEY,
  SIGNED_PREKEY_SIGNATURE_INVALID,
  hkdf,
  deriveMessageKeys,
  aesEncrypt,
  aesDecrypt,
  hmacSha256,
  sha256,
  generateSafetyNumber,
  fingerprint,
} from './CryptoUtils';
export type { ECKeyPair, ExportedKeyPair, EncryptedPayload } from './CryptoUtils';

export { E2EEManager, getE2EEManager } from './E2EEManager';
export type {
  PreKeyBundle,
  SignalEnvelope,
  E2EEStatus,
  SessionInfo,
} from './E2EEManager';
