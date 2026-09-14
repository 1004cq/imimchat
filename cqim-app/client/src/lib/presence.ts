/**
 * APNs 前台/后台 presence 上报。
 * 仅 presence=foreground 会跳过推送；WebSocket 在线不算免推。
 */
export type PresenceState = 'foreground' | 'background' | 'offline';

/** Redis key TTL 为 90s，心跳需更短以续期。 */
export const PRESENCE_HEARTBEAT_MS = 45_000;

export function pageIsForeground(): boolean {
  if (typeof document === 'undefined') return true;
  return document.visibilityState === 'visible';
}

export function resolvePresenceState(): PresenceState {
  return pageIsForeground() ? 'foreground' : 'background';
}

export async function reportPresence(
  state: PresenceState,
  activeChatId?: string | null,
  opts?: { keepalive?: boolean },
): Promise<void> {
  const token = localStorage.getItem('user_token') || localStorage.getItem('auth_token');
  if (!token) return;

  const body = JSON.stringify({
    state,
    activeChatId: activeChatId || null,
  });

  try {
    await fetch('/api/presence', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
      },
      body,
      keepalive: opts?.keepalive === true,
    });
  } catch {
    // 后台/卸载时允许失败；下次心跳或可见时会重试
  }
}
