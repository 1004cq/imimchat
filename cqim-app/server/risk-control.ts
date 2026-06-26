/**
 * 风控服务 — 架构图「反垃圾 / 限流 / 封禁」
 *
 * 能力：
 * 1. 敏感词过滤（复用 SensitiveWord 表）
 * 2. 消息频率限流（防刷屏）
 * 3. 重复内容检测
 * 4. 用户/群消息发送前校验
 */

import { Router, Request, Response } from 'express';
import prisma from './db.js';
import { publishEvent } from './mq.js';

const riskRouter = Router();

// ============ 内存缓存 ============

let sensitiveWordsCache: string[] = [];
let sensitiveWordsLoadedAt = 0;
const SENSITIVE_CACHE_TTL = 60_000; // 1 分钟刷新

interface RateLimitEntry {
  count: number;
  resetAt: number;
  lastContent?: string;
  repeatCount: number;
}

const messageRateStore = new Map<string, RateLimitEntry>();

// ============ 配置 ============

export interface RiskConfig {
  /** 每分钟最大消息数（单用户） */
  maxMessagesPerMinute: number;
  /** 重复内容触发阈值 */
  maxRepeatCount: number;
  /** 是否启用敏感词过滤 */
  enableSensitiveFilter: boolean;
  /** 是否启用频率限制 */
  enableRateLimit: boolean;
}

const DEFAULT_RISK_CONFIG: RiskConfig = {
  maxMessagesPerMinute: 30,
  maxRepeatCount: 5,
  enableSensitiveFilter: true,
  enableRateLimit: true,
};

let riskConfig: RiskConfig = { ...DEFAULT_RISK_CONFIG };

export function getRiskConfig(): RiskConfig {
  return { ...riskConfig };
}

export async function loadRiskConfig(): Promise<void> {
  try {
    const cfg = await prisma.systemConfig.findUnique({ where: { key: 'risk_control' } });
    if (cfg?.value) {
      riskConfig = { ...DEFAULT_RISK_CONFIG, ...JSON.parse(cfg.value) };
    }
  } catch {
    // 使用默认配置
  }
}

// ============ 敏感词 ============

async function loadSensitiveWords(): Promise<string[]> {
  const now = Date.now();
  if (now - sensitiveWordsLoadedAt < SENSITIVE_CACHE_TTL && sensitiveWordsCache.length > 0) {
    return sensitiveWordsCache;
  }
  const words = await prisma.sensitiveWord.findMany({
    where: { isActive: true },
    select: { word: true },
  });
  sensitiveWordsCache = words.map(w => w.word.toLowerCase());
  sensitiveWordsLoadedAt = now;
  return sensitiveWordsCache;
}

export async function checkSensitiveWords(text: string): Promise<{ blocked: boolean; matched?: string }> {
  if (!riskConfig.enableSensitiveFilter || !text) {
    return { blocked: false };
  }
  const words = await loadSensitiveWords();
  const lower = text.toLowerCase();
  for (const word of words) {
    if (lower.includes(word)) {
      return { blocked: true, matched: word };
    }
  }
  return { blocked: false };
}

// ============ 频率限制 ============

function getRateLimitKey(userId: string, scope: string): string {
  return `${userId}:${scope}`;
}

export function checkMessageRate(userId: string, scope: string, content: string): {
  allowed: boolean;
  reason?: string;
  retryAfter?: number;
} {
  if (!riskConfig.enableRateLimit) {
    return { allowed: true };
  }

  const key = getRateLimitKey(userId, scope);
  const now = Date.now();
  const windowMs = 60_000;
  let entry = messageRateStore.get(key);

  if (!entry || now > entry.resetAt) {
    entry = { count: 0, resetAt: now + windowMs, repeatCount: 0 };
    messageRateStore.set(key, entry);
  }

  entry.count++;

  // 频率超限
  if (entry.count > riskConfig.maxMessagesPerMinute) {
    const retryAfter = Math.ceil((entry.resetAt - now) / 1000);
    return { allowed: false, reason: '发送过于频繁，请稍后再试', retryAfter };
  }

  // 重复内容检测
  const normalized = content.trim().toLowerCase();
  if (normalized && normalized === entry.lastContent) {
    entry.repeatCount++;
    if (entry.repeatCount >= riskConfig.maxRepeatCount) {
      return { allowed: false, reason: '请勿重复发送相同内容' };
    }
  } else {
    entry.lastContent = normalized;
    entry.repeatCount = 1;
  }

  return { allowed: true };
}

// ============ 综合校验 ============

export interface MessageCheckInput {
  userId: string;
  scope: string; // private:{chatId} | group:{groupId} | channel:{channelId}
  content: string;
  msgType?: string;
}

export interface MessageCheckResult {
  allowed: boolean;
  reason?: string;
  retryAfter?: number;
  riskLevel?: 'low' | 'medium' | 'high';
}

export async function checkMessage(input: MessageCheckInput): Promise<MessageCheckResult> {
  const { userId, scope, content, msgType } = input;

  // 系统消息跳过
  if (msgType === 'system') {
    return { allowed: true, riskLevel: 'low' };
  }

  // 检查用户是否被封禁
  const user = await prisma.user.findUnique({
    where: { id: userId },
    select: { isBanned: true, banReason: true },
  });
  if (user?.isBanned) {
    return { allowed: false, reason: user.banReason || '账号已被封禁', riskLevel: 'high' };
  }

  // 敏感词
  const textToCheck = typeof content === 'string' ? content : JSON.stringify(content);
  const sensitive = await checkSensitiveWords(textToCheck);
  if (sensitive.blocked) {
    void publishEvent('risk.alert', {
      type: 'sensitive_word',
      userId,
      scope,
      matched: sensitive.matched,
    });
    return { allowed: false, reason: '消息包含违规内容', riskLevel: 'high' };
  }

  // 频率限制
  const rate = checkMessageRate(userId, scope, textToCheck);
  if (!rate.allowed) {
    void publishEvent('risk.alert', {
      type: 'rate_limit',
      userId,
      scope,
      retryAfter: rate.retryAfter,
    });
    return { allowed: false, reason: rate.reason, retryAfter: rate.retryAfter, riskLevel: 'medium' };
  }

  return { allowed: true, riskLevel: 'low' };
}

// ============ 慢速模式 ============

export async function checkSlowMode(
  groupId: string,
  userId: string,
  memberRole: string,
): Promise<{ allowed: boolean; retryAfter?: number }> {
  const group = await prisma.group.findUnique({
    where: { id: groupId },
    select: { slowModeSeconds: true, type: true },
  });

  if (!group || group.slowModeSeconds <= 0) {
    return { allowed: true };
  }

  // 管理员和群主不受慢速模式限制
  if (memberRole === 'owner' || memberRole === 'admin') {
    return { allowed: true };
  }

  const lastMsg = await prisma.groupMessage.findFirst({
    where: { groupId, senderId: userId, isRevoked: false },
    orderBy: { createdAt: 'desc' },
    select: { createdAt: true },
  });

  if (!lastMsg) {
    return { allowed: true };
  }

  const elapsed = (Date.now() - lastMsg.createdAt.getTime()) / 1000;
  if (elapsed < group.slowModeSeconds) {
    return {
      allowed: false,
      retryAfter: Math.ceil(group.slowModeSeconds - elapsed),
    };
  }

  return { allowed: true };
}

// ============ API 路由 ============

/** GET /api/risk/config — 获取风控配置（管理员） */
riskRouter.get('/config', async (_req: Request, res: Response) => {
  await loadRiskConfig();
  const wordCount = await prisma.sensitiveWord.count({ where: { isActive: true } });
  res.json({
    config: getRiskConfig(),
    sensitiveWordCount: wordCount,
    activeRateLimits: messageRateStore.size,
  });
});

/** PUT /api/risk/config — 更新风控配置 */
riskRouter.put('/config', async (req: Request, res: Response) => {
  const updates = req.body;
  riskConfig = { ...riskConfig, ...updates };
  await prisma.systemConfig.upsert({
    where: { key: 'risk_control' },
    create: { key: 'risk_control', value: JSON.stringify(riskConfig) },
    update: { value: JSON.stringify(riskConfig) },
  });
  res.json({ success: true, config: getRiskConfig() });
});

/** POST /api/risk/check — 手动检测内容（调试用） */
riskRouter.post('/check', async (req: Request, res: Response) => {
  const { userId, scope, content } = req.body;
  if (!userId || !content) {
    return res.status(400).json({ error: '缺少 userId 或 content' });
  }
  const result = await checkMessage({ userId, scope: scope || 'test', content });
  res.json(result);
});

/** 定期清理过期的频率限制记录 */
setInterval(() => {
  const now = Date.now();
  for (const [key, entry] of messageRateStore) {
    if (now > entry.resetAt) {
      messageRateStore.delete(key);
    }
  }
}, 120_000);

export default riskRouter;
