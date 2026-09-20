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
