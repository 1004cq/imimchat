/**
 * 可读账号展示：username > wechatId > phone，永不展示内部 cuid。
 */

function isInternalIdLike(value: string, internalId?: string | null): boolean {
  const v = value.trim();
  if (!v) return true;
  const internal = (internalId || '').trim();
  if (internal && v === internal) return true;
  // Prisma/cuid 风格：以 c 开头的长串（如 cmnv23txe…）
  if (/^c[a-z0-9]{20,}$/i.test(v)) return true;
  return false;
}

export function resolveDisplayAccountId(input: {
  username?: string | null;
  wechatId?: string | null;
  phone?: string | null;
  /** 内部 uid，即使出现在其他字段也不展示 */
  userId?: string | null;
}): string | null {
  for (const raw of [input.username, input.wechatId, input.phone]) {
    const v = (raw || '').trim();
    if (!v) continue;
    if (isInternalIdLike(v, input.userId)) continue;
    return v;
  }
  return null;
}

/** 文案：「账号：xxx」；无可读 ID 时返回 null（不展示 cuid 截断） */
export function formatAccountLabel(account: string | null | undefined): string | null {
  const v = (account || '').trim();
  if (!v) return null;
  return `账号：${v}`;
}
