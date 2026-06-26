/**
 * Bot 平台 — 类 Telegram Bot API
 *
 * 提供 Token 管理、Webhook、Polling、消息发送等核心能力。
 * 兼容现有 OneBot 适配层，Bot 用户通过 User.isBot=true 标识。
 *
 * API 风格参考 Telegram Bot API：
 * - POST /api/bot/create          创建 Bot
 * - GET  /bot{token}/getMe        获取 Bot 信息
 * - POST /bot{token}/sendMessage  发送消息
 * - POST /bot{token}/setWebhook   设置 Webhook
 * - GET  /bot{token}/getUpdates   长轮询获取更新
 */

import { Router, Request, Response, NextFunction } from 'express';
import crypto from 'crypto';
import prisma, { hashPassword } from './db.js';
import { generateUserDialogId, dialogIdToString } from './utils/peerId.js';
import { publishEvent } from './mq.js';
import { avatarToProxy } from './cos-signer.js';

const botRouter = Router();

// ============ Token 工具 ============

function generateBotToken(botUserId: string): string {
  const secret = crypto.randomBytes(24).toString('hex');
  return `${botUserId}:${secret}`;
}

function parseBotToken(token: string): { botUserId: string; secret: string } | null {
  const idx = token.indexOf(':');
  if (idx === -1) return null;
  return { botUserId: token.slice(0, idx), secret: token.slice(idx + 1) };
}

// ============ 认证中间件 ============

async function botAuthMiddleware(req: Request, res: Response, next: NextFunction) {
  const token = req.params.token || req.headers['x-bot-token'] as string;
  if (!token) {
    return res.status(401).json({ ok: false, error_code: 401, description: 'Unauthorized' });
  }

  const botToken = await prisma.botToken.findUnique({
    where: { token },
  });

  if (!botToken || !botToken.isActive) {
    return res.status(401).json({ ok: false, error_code: 401, description: 'Invalid bot token' });
  }

  const botUser = await prisma.user.findUnique({
    where: { id: botToken.botUserId },
    select: { id: true, username: true, nickname: true, avatar: true, isBot: true, isBanned: true },
  });

  if (!botUser || !botUser.isBot || botUser.isBanned) {
    return res.status(401).json({ ok: false, error_code: 401, description: 'Bot not found or disabled' });
  }

  (req as any).botToken = botToken;
  (req as any).botUser = botUser;
  next();
}

// ============ 更新队列 ============

let globalUpdateCounter = 1;

async function enqueueBotUpdate(botUserId: string, update: object): Promise<number> {
  const updateId = globalUpdateCounter++;
  await prisma.botUpdate.create({
    data: {
      botUserId,
      updateId,
      payload: JSON.stringify(update),
    },
  });
  void publishEvent('bot.update', { botUserId, updateId, update });
  return updateId;
}

async function deliverWebhook(botToken: typeof prisma.botToken extends { findUnique: (...args: any) => Promise<infer T> } ? T : never, update: object): Promise<boolean> {
  if (!botToken?.webhookUrl) return false;

  try {
    const body = JSON.stringify(update);
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
    };
    if (botToken.webhookSecret) {
      headers['X-Telegram-Bot-Api-Secret-Token'] = botToken.webhookSecret;
    }

    const response = await fetch(botToken.webhookUrl, {
      method: 'POST',
      headers,
      body,
      signal: AbortSignal.timeout(10_000),
    });

    return response.ok;
  } catch (err) {
    console.error(`[Bot] Webhook 投递失败:`, err);
    return false;
  }
}

// ============ 消息广播（复用现有基础设施） ============

async function broadcastBotMessage(params: {
  botUserId: string;
  botName: string;
  chatId: string;
  chatType: 'private' | 'group' | 'channel';
  text: string;
  parseMode?: string;
}): Promise<{ messageId: string }> {
  const messageId = `bot-${Date.now()}-${crypto.randomBytes(4).toString('hex')}`;
  const timestamp = Date.now();

  const wsMessage = {
    type: 'bot_message',
    chatId: params.chatId,
    senderId: params.botUserId,
    senderName: params.botName,
    content: params.text,
    messageId,
    timestamp,
    msgType: 'text',
  };

  // 通过 Redis 广播（index.ts 中的 WebSocket 连接会接收）
  const { publishMessage } = await import('./redis.js');
  await publishMessage(`user:${params.chatId}`, wsMessage);

  return { messageId };
}

// ============ 管理 API ============

/** POST /api/bot/create — 创建 Bot */
botRouter.post('/create', async (req: Request, res: Response) => {
  try {
    const { ownerId, name, username, description, avatar } = req.body;
    if (!ownerId || !name || !username) {
      return res.status(400).json({ error: '缺少 ownerId, name 或 username' });
    }

    if (!/^[a-zA-Z][a-zA-Z0-9_]{4,31}$/.test(username)) {
      return res.status(400).json({ error: 'Bot 用户名格式不正确' });
    }

    const existing = await prisma.user.findUnique({ where: { username } });
    if (existing) {
      return res.status(409).json({ error: '用户名已被占用' });
    }

    const dialogId = generateUserDialogId(true);
    const dialogIdStr = dialogIdToString(dialogId);

    const botUser = await prisma.user.create({
      data: {
        username,
        password: hashPassword(crypto.randomBytes(32).toString('hex')),
        nickname: name,
        bio: description || '',
        avatar: avatar || null,
        isBot: true,
        dialogId: dialogIdStr,
        phoneVerified: true,
      },
    });

    const token = generateBotToken(botUser.id);
    const botToken = await prisma.botToken.create({
      data: {
        botUserId: botUser.id,
        token,
        ownerId,
      },
    });

    res.json({
      ok: true,
      bot: {
        id: botUser.id,
        dialogId: dialogIdStr,
        username: botUser.username,
        name: botUser.nickname,
        isBot: true,
      },
      token: botToken.token,
    });
  } catch (err: any) {
    res.status(500).json({ error: err.message });
  }
});

/** GET /api/bot/list — 列出用户创建的 Bot */
botRouter.get('/list', async (req: Request, res: Response) => {
  const { ownerId } = req.query as Record<string, string>;
  if (!ownerId) return res.status(400).json({ error: '缺少 ownerId' });

  const bots = await prisma.botToken.findMany({
    where: { ownerId },
    orderBy: { createdAt: 'desc' },
  });

  const botUsers = await prisma.user.findMany({
    where: { id: { in: bots.map(b => b.botUserId) } },
    select: { id: true, username: true, nickname: true, avatar: true, bio: true, dialogId: true },
  });
  const userMap = new Map(botUsers.map(u => [u.id, u]));

  res.json({
    bots: bots.map(b => {
      const user = userMap.get(b.botUserId);
      return {
        id: b.id,
        botUserId: b.botUserId,
        username: user?.username,
        name: user?.nickname,
        avatar: avatarToProxy(user?.avatar),
        isActive: b.isActive,
        hasWebhook: !!b.webhookUrl,
        createdAt: b.createdAt,
      };
    }),
  });
});

/** DELETE /api/bot/:botUserId — 删除 Bot */
botRouter.delete('/:botUserId', async (req: Request, res: Response) => {
  const { botUserId } = req.params;
  const { ownerId } = req.body;
  if (!ownerId) return res.status(400).json({ error: '缺少 ownerId' });

  const botToken = await prisma.botToken.findUnique({ where: { botUserId } });
  if (!botToken || botToken.ownerId !== ownerId) {
    return res.status(404).json({ error: 'Bot 不存在或无权限' });
  }

  await prisma.$transaction([
    prisma.botUpdate.deleteMany({ where: { botUserId } }),
    prisma.botToken.delete({ where: { botUserId } }),
    prisma.user.delete({ where: { id: botUserId } }),
  ]);

  res.json({ ok: true });
});

// ============ Telegram 风格 Bot API ============

const botApiRouter = Router();

/** GET /bot{token}/getMe */
botApiRouter.get('/:token/getMe', botAuthMiddleware, (req: Request, res: Response) => {
  const bot = (req as any).botUser;
  res.json({
    ok: true,
    result: {
      id: bot.id,
      is_bot: true,
      first_name: bot.nickname || bot.username,
      username: bot.username,
    },
  });
});

/** POST /bot{token}/sendMessage */
botApiRouter.post('/:token/sendMessage', botAuthMiddleware, async (req: Request, res: Response) => {
  try {
    const bot = (req as any).botUser;
    const { chat_id, text, parse_mode } = req.body;

    if (!chat_id || !text) {
      return res.status(400).json({ ok: false, error_code: 400, description: 'chat_id and text are required' });
    }

    const { messageId } = await broadcastBotMessage({
      botUserId: bot.id,
      botName: bot.nickname || bot.username,
      chatId: String(chat_id),
      chatType: 'private',
      text,
      parseMode: parse_mode,
    });

    res.json({
      ok: true,
      result: {
        message_id: messageId,
        from: { id: bot.id, is_bot: true, first_name: bot.nickname, username: bot.username },
        chat: { id: chat_id, type: 'private' },
        date: Math.floor(Date.now() / 1000),
        text,
      },
    });
  } catch (err: any) {
    res.status(500).json({ ok: false, error_code: 500, description: err.message });
  }
});

/** POST /bot{token}/setWebhook */
botApiRouter.post('/:token/setWebhook', botAuthMiddleware, async (req: Request, res: Response) => {
  const botToken = (req as any).botToken;
  const { url, secret_token, allowed_updates } = req.body;

  if (!url) {
    return res.status(400).json({ ok: false, error_code: 400, description: 'url is required' });
  }

  await prisma.botToken.update({
    where: { id: botToken.id },
    data: {
      webhookUrl: url,
      webhookSecret: secret_token || null,
      allowedUpdates: allowed_updates ? JSON.stringify(allowed_updates) : null,
    },
  });

  res.json({ ok: true, result: true, description: 'Webhook was set' });
});

/** POST /bot{token}/deleteWebhook */
botApiRouter.post('/:token/deleteWebhook', botAuthMiddleware, async (req: Request, res: Response) => {
  const botToken = (req as any).botToken;
  await prisma.botToken.update({
    where: { id: botToken.id },
    data: { webhookUrl: null, webhookSecret: null },
  });
  res.json({ ok: true, result: true, description: 'Webhook was deleted' });
});

/** GET /bot{token}/getWebhookInfo */
botApiRouter.get('/:token/getWebhookInfo', botAuthMiddleware, (req: Request, res: Response) => {
  const botToken = (req as any).botToken;
  res.json({
    ok: true,
    result: {
      url: botToken.webhookUrl || '',
      has_custom_certificate: false,
      pending_update_count: 0,
    },
  });
});

/** GET /bot{token}/getUpdates — 长轮询 */
botApiRouter.get('/:token/getUpdates', botAuthMiddleware, async (req: Request, res: Response) => {
  const botToken = (req as any).botToken;
  const offset = parseInt(req.query.offset as string) || 0;
  const limit = Math.min(parseInt(req.query.limit as string) || 100, 100);
  const timeout = Math.min(parseInt(req.query.timeout as string) || 0, 50);

  const fetchUpdates = async () => {
    return prisma.botUpdate.findMany({
      where: {
        botUserId: botToken.botUserId,
        processed: false,
        updateId: { gt: offset },
      },
      orderBy: { updateId: 'asc' },
      take: limit,
    });
  };

  let updates = await fetchUpdates();

  // 长轮询等待
  if (updates.length === 0 && timeout > 0) {
    const deadline = Date.now() + timeout * 1000;
    while (Date.now() < deadline) {
      await new Promise(r => setTimeout(r, 500));
      updates = await fetchUpdates();
      if (updates.length > 0) break;
    }
  }

  if (updates.length > 0) {
    await prisma.botUpdate.updateMany({
      where: { id: { in: updates.map(u => u.id) } },
      data: { processed: true },
    });
  }

  res.json({
    ok: true,
    result: updates.map(u => JSON.parse(u.payload)),
  });
});

export { enqueueBotUpdate, deliverWebhook, botApiRouter };
export default botRouter;
