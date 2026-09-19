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
  try {
    const raw = await redis.get(presenceKey(userId));
    if (raw === 'foreground' || raw === 'background') return raw;
    return 'offline';
  } catch (error) {
    console.error(`[presence] Redis 读取状态失败 userId=${userId}，保守按 offline 处理:`, error);
    return 'offline';
  }
}

export async function getActiveChatId(userId: string): Promise<string | null> {
  try {
    const raw = await redis.get(activeChatKey(userId));
    return raw || null;
  } catch (error) {
    console.error(`[presence] Redis 读取 activeChatId 失败 userId=${userId}:`, error);
    return null;
  }
}

export async function setPresence(userId: string, state: Presence, activeChatId?: string | null) {
  if (state === 'offline') {
    await redis.del(presenceKey(userId), activeChatKey(userId));
    return;
  }

  await redis.set(presenceKey(userId), state, 'EX', PRESENCE_TTL_SECONDS);

  if (typeof activeChatId === 'string' && activeChatId.length > 0) {
    await redis.set(activeChatKey(userId), activeChatId, 'EX', PRESENCE_TTL_SECONDS);
  } else if (activeChatId === null || activeChatId === '') {
    await redis.del(activeChatKey(userId));
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
