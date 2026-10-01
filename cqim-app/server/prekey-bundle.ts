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

const MAX_CONSUMED_PREKEY_IDS = 2048;

export function normalizeConsumedPreKeyIds(value: unknown): number[] {
  if (!Array.isArray(value)) return [];
  const ids = new Set<number>();
  for (const item of value) {
    if (typeof item === 'number' && Number.isFinite(item)) ids.add(item);
  }
  return Array.from(ids).slice(-MAX_CONSUMED_PREKEY_IDS);
}

export function rememberConsumedPreKey(value: unknown, keyId: number): number[] {
  return normalizeConsumedPreKeyIds([...normalizeConsumedPreKeyIds(value), keyId]);
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

/**
 * Returns one valid one-time key and a pool that no longer contains it.
 * Keeping this operation explicit prevents callers from accidentally sending
 * the same key and writing it back to storage afterwards.
 */
export function takeOneValidP256PreKey(value: unknown, consumedIds: unknown = []): {
  preKey: StoredPreKey | null;
  remaining: StoredPreKey[];
} {
  const consumed = new Set(normalizeConsumedPreKeyIds(consumedIds));
  const [preKey, ...remaining] = filterValidP256PreKeys(value)
    .filter(item => !consumed.has(item.keyId));
  return { preKey: preKey ?? null, remaining };
}

export function mergeValidP256PreKeys(
  existing: unknown,
  incoming: unknown,
  consumedIds: unknown = [],
): StoredPreKey[] {
  const consumed = new Set(normalizeConsumedPreKeyIds(consumedIds));
  const keyMap = new Map<number, string>();
  for (const item of [...filterValidP256PreKeys(existing), ...filterValidP256PreKeys(incoming)]) {
    if (!consumed.has(item.keyId)) keyMap.set(item.keyId, item.publicKey);
  }
  return Array.from(keyMap.entries()).map(([keyId, publicKey]) => ({ keyId, publicKey }));
}
