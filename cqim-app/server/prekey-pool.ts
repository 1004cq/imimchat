/** A consumed key ID is a tombstone for this identity, not an empty slot. */
export type PublicPreKey = { keyId: number; publicKey: string };
export type PreKeyPool = { available: PublicPreKey[]; consumed: number[] };

export function mergePreKeys(pool: PreKeyPool, incoming: PublicPreKey[]): PreKeyPool {
  const used = new Set(pool.consumed);
  const keys = new Map(pool.available.map(key => [key.keyId, key]));
  for (const key of incoming) {
    if (!Number.isSafeInteger(key.keyId) || key.keyId < 0 || typeof key.publicKey !== 'string' || !key.publicKey) {
      throw new Error('Invalid PreKey');
    }
    if (used.has(key.keyId)) continue;
    const previous = keys.get(key.keyId);
    if (previous && previous.publicKey !== key.publicKey) throw new Error('PreKey ID conflict');
    keys.set(key.keyId, key);
  }
  return { available: [...keys.values()], consumed: [...used] };
}

export function consumePreKey(pool: PreKeyPool): { pool: PreKeyPool; key?: PublicPreKey } {
  const [key, ...available] = pool.available;
  if (!key) return { pool };
  return { key, pool: { available, consumed: [...new Set([...pool.consumed, key.keyId])] } };
}
