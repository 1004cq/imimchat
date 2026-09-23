export type OneTimePreKey = { keyId: number; publicKey: string };

const DEFAULT_PREKEY_CAP = 100;
const DEFAULT_CONSUMED_CAP = 5000;

/** 记录已下发的 One-Time PreKey，避免客户端再次上传时把已消费的公钥发回去 */
export function rememberConsumedPreKey(existing: number[], keyId: number, cap = DEFAULT_CONSUMED_CAP): number[] {
  const next = existing.filter(id => id !== keyId);
  next.push(keyId);
  return next.slice(-cap);
}

/**
 * 合并上传的 One-Time PreKey。
 * 已消费的 keyId 一律丢弃；超出上限时保留 keyId 最大的一批（新密钥必须单调递增）。
 */
export function mergeAndCapPreKeys(
  existingKeys: OneTimePreKey[],
  newKeys: OneTimePreKey[],
  consumedIds: number[] = [],
  cap = DEFAULT_PREKEY_CAP,
): OneTimePreKey[] {
  const consumed = new Set(consumedIds);
  const keyMap = new Map<number, string>();
  for (const key of existingKeys) {
    if (!consumed.has(key.keyId)) keyMap.set(key.keyId, key.publicKey);
  }
  for (const key of newKeys) {
    if (!consumed.has(key.keyId)) keyMap.set(key.keyId, key.publicKey);
  }
  return Array.from(keyMap.entries())
    .map(([keyId, publicKey]) => ({ keyId, publicKey }))
    .sort((a, b) => a.keyId - b.keyId)
    .slice(-cap);
}

/**
 * 从库存里取一把尚未消费的 One-Time PreKey。
 * 取出的 id 同时写入 consumed，后续上传不能把它放回可下发列表。
 */
export function selectPreKeyForIssue(
  preKeys: OneTimePreKey[],
  consumedIds: number[],
): { taken?: OneTimePreKey; rest: OneTimePreKey[]; consumed: number[] } {
  const consumed = new Set(consumedIds);
  const available = preKeys.filter(key => !consumed.has(key.keyId));
  const taken = available[0];
  if (!taken) {
    return { rest: available, consumed: consumedIds };
  }
  return {
    taken,
    rest: available.slice(1),
    consumed: rememberConsumedPreKey(consumedIds, taken.keyId),
  };
}

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
