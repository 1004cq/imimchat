import { Router, Request, Response } from 'express';
import { redis } from './redis.js';
import { userAuth } from './auth.js';
import {
  parsePresenceBody,
  PRESENCE_TTL_SECONDS,
  shouldSkipApnsFromState,
  type Presence,
} from './presence-rules.js';

export {
  isPresence,
  parsePresenceBody,
  PRESENCE_STATES,
  PRESENCE_TTL_SECONDS,
  shouldSkipApnsFromState,
  type Presence,
} from './presence-rules.js';

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

const router = Router();
router.use(userAuth);

/**
 * POST /api/presence
 * Body: { state: foreground|background|offline, activeChatId?: string | null }
 * Redis TTL 90s；客户端需心跳续期。WebSocket 在线 ≠ 跳过推送。
 */
router.post('/', async (req: Request, res: Response) => {
  try {
    const currentUser = (req as any).user;
    const parsed = parsePresenceBody(req.body);
    if (!parsed.ok) {
      return res.status(400).json({ error: parsed.error });
    }

    await setPresence(currentUser.id, parsed.state, parsed.activeChatId);
    return res.json({
      success: true,
      state: parsed.state,
      activeChatId: parsed.activeChatId,
      ttlSeconds: PRESENCE_TTL_SECONDS,
    });
  } catch (err) {
    console.error('[presence] 设置失败:', err);
    return res.status(500).json({ error: '服务器内部错误' });
  }
});

/**
 * GET /api/presence
 * 当前登录用户的 APNs 前台/后台状态（不是 Gateway WS online）。
 */
router.get('/', async (req: Request, res: Response) => {
  try {
    const currentUser = (req as any).user;
    const [state, activeChatId] = await Promise.all([
      getPresence(currentUser.id),
      getActiveChatId(currentUser.id),
    ]);
    return res.json({
      state,
      activeChatId,
      skipApns: shouldSkipApnsFromState(state),
    });
  } catch (err) {
    console.error('[presence] 读取失败:', err);
    return res.status(500).json({ error: '服务器内部错误' });
  }
});

export default router;
