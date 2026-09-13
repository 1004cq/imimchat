export type Presence = 'foreground' | 'background' | 'offline';

export const PRESENCE_TTL_SECONDS = 90;
export const PRESENCE_STATES: readonly Presence[] = ['foreground', 'background', 'offline'];

export function isPresence(value: unknown): value is Presence {
  return value === 'foreground' || value === 'background' || value === 'offline';
}

export function parsePresenceBody(body: unknown):
  | { ok: true; state: Presence; activeChatId?: string | null }
  | { ok: false; error: string } {
  const raw = body && typeof body === 'object' ? (body as Record<string, unknown>) : {};
  if (!isPresence(raw.state)) {
    return { ok: false, error: 'state 必须是 foreground、background 或 offline' };
  }
  if (raw.activeChatId === null || raw.activeChatId === '') {
    return { ok: true, state: raw.state, activeChatId: null };
  }
  if (typeof raw.activeChatId === 'string') {
    return { ok: true, state: raw.state, activeChatId: raw.activeChatId };
  }
  return { ok: true, state: raw.state };
}

/** 纯规则：仅 foreground 跳过 APNs。WS 在线不参与判断。 */
export function shouldSkipApnsFromState(
  state: Presence,
  chatId?: string,
  activeChatId?: string | null,
): boolean {
  if (state !== 'foreground') return false;
  if (!chatId) return true;
  if (!activeChatId) return true;
  return activeChatId === chatId;
}
