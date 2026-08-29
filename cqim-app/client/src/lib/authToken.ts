/** 读取登录 token（兼容 user_token / auth_token） */
export function getAuthToken(): string | null {
  try {
    return localStorage.getItem('user_token') || localStorage.getItem('auth_token');
  } catch {
    return null;
  }
}

/** 当前用户 ID，排除占位符 me */
export function getCurrentUserId(fallback?: string | null): string {
  const id = fallback || localStorage.getItem('user_id') || 'me';
  return id && id !== 'me' ? id : (localStorage.getItem('user_id') || id);
}

/** 从会话 members 解析对端 ID */
export function resolveOtherMember(
  members: string[] | undefined,
  currentUserId: string,
): string | undefined {
  if (!members?.length) return undefined;
  const selfIds = new Set([currentUserId, 'me'].filter(Boolean));
  return members.find((m) => m && !selfIds.has(m));
}
