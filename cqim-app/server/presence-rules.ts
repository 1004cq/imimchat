export type Presence = 'foreground' | 'background' | 'offline';

export const PRESENCE_TTL_SECONDS = 90;
export const PRESENCE_STATES: readonly Presence[] = ['foreground', 'background', 'offline'];

export function isPresence(value: unknown): value is Presence {
  return value === 'foreground' || value === 'background' || value === 'offline';
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
