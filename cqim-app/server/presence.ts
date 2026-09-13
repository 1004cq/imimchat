import { redis } from './redis.js';

export type Presence = 'foreground' | 'background' | 'offline';

export async function getPresence(userId: string): Promise<Presence> {
  const raw = await redis.get(`user:presence:${userId}`).catch(() => null);
  if (raw === 'foreground' || raw === 'background') return raw;
  return 'offline';
}

export async function setPresence(userId: string, state: Presence, activeChatId?: string) {
  await redis.set(`user:presence:${userId}`, state, 'EX', 90).catch(() => undefined);
  if (activeChatId) {
    await redis.set(`user:activeChat:${userId}`, activeChatId, 'EX', 90).catch(() => undefined);
  }
}

/** 仅前台才跳过 APNs。有 WS 不算免推。 */
export async function shouldSkipApns(userId: string, chatId?: string): Promise<boolean> {
  const state = await getPresence(userId);
  if (state !== 'foreground') return false;
  if (!chatId) return true;
  const active = await redis.get(`user:activeChat:${userId}`).catch(() => null);
  if (!active) return true;
  return active === chatId;
}
