import { createPublicKey } from 'node:crypto';

/** 合并 PreKey Bundle 中的 ECDSA 签名公钥，避免旧客户端把已发布字段覆盖成空 */
export function resolveSigningPublicKey(
  incoming: unknown,
  identityKey: string,
  existingValue?: string | null,
): string | null {
  if (typeof incoming === 'string' && incoming.length > 0) return incoming;
  if (!existingValue) return null;
  try {
    const prev = JSON.parse(existingValue);
    if (prev.identityKey === identityKey && typeof prev.signingPublicKey === 'string' && prev.signingPublicKey) {
      return prev.signingPublicKey;
    }
  } catch {}
  return null;
}

export interface StoredPreKey {
  keyId: number;
  publicKey: string;
}

/** Web 端只接受 WebCrypto 导出的 P-256 SPKI，旧的 32 字节 raw key 必须丢弃。 */
export function isValidP256SPKIPublicKey(value: unknown): value is string {
  if (typeof value !== 'string' || value.length === 0) return false;
  try {
    const der = Buffer.from(value, 'base64');
    const normalizedInput = value.replace(/\s+/g, '').replace(/=+$/g, '');
    if (!der.length || der.toString('base64').replace(/=+$/g, '') !== normalizedInput) return false;
    const key = createPublicKey({ key: der, format: 'der', type: 'spki' });
    return key.asymmetricKeyType === 'ec'
      && (key.asymmetricKeyDetails?.namedCurve === 'prime256v1'
        || key.asymmetricKeyDetails?.namedCurve === 'P-256');
  } catch {
    return false;
  }
}

export function filterValidP256PreKeys(value: unknown): StoredPreKey[] {
  if (!Array.isArray(value)) return [];
  return value.filter((item): item is StoredPreKey => (
    !!item
    && typeof item === 'object'
    && Number.isFinite((item as StoredPreKey).keyId)
    && isValidP256SPKIPublicKey((item as StoredPreKey).publicKey)
  ));
}

export function mergeValidP256PreKeys(existing: unknown, incoming: unknown): StoredPreKey[] {
  const keyMap = new Map<number, string>();
  for (const item of [...filterValidP256PreKeys(existing), ...filterValidP256PreKeys(incoming)]) {
    keyMap.set(item.keyId, item.publicKey);
  }
  return Array.from(keyMap.entries()).map(([keyId, publicKey]) => ({ keyId, publicKey }));
}
