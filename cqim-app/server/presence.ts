import { redis } from './redis.js';
import {
  PRESENCE_TTL_SECONDS,
  shouldSkipApnsFromState,
  type Presence,
} from './presence-rules.js';

export type { Presence };
export { PRESENCE_TTL_SECONDS, shouldSkipApnsFromState };

function presenceKey(userId: string) {
  return `user:presence:${userId}`;
}

function activeChatKey(userId: string) {
  return `user:activeChat:${userId}`;
}

export async function getPresence(userId: string): Promise<Presence> {
  const raw = await redis.get(presenceKey(userId)).catch(() => null);
  if (raw === 'foreground' || raw === 'background') return raw;
  return 'offline';
}

export async function getActiveChatId(userId: string): Promise<string | null> {
  const raw = await redis.get(activeChatKey(userId)).catch(() => null);
  return raw || null;
}

export async function setPresence(userId: string, state: Presence, activeChatId?: string | null) {
  if (state === 'offline') {
    await redis.del(presenceKey(userId), activeChatKey(userId)).catch(() => undefined);
    return;
  }

  await redis.set(presenceKey(userId), state, 'EX', PRESENCE_TTL_SECONDS).catch(() => undefined);

  if (typeof activeChatId === 'string' && activeChatId.length > 0) {
    await redis.set(activeChatKey(userId), activeChatId, 'EX', PRESENCE_TTL_SECONDS).catch(() => undefined);
  } else if (activeChatId === null || activeChatId === '') {
    await redis.del(activeChatKey(userId)).catch(() => undefined);
  }
}

/** 仅前台才跳过 APNs。有 WS 不算免推。 */
export async function shouldSkipApns(userId: string, chatId?: string): Promise<boolean> {
  const state = await getPresence(userId);
  if (state !== 'foreground') return false;
  if (!chatId) return true;
  const active = await getActiveChatId(userId);
  return shouldSkipApnsFromState(state, chatId, active);
}
