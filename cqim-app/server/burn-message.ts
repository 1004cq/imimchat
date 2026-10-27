/**
 * server/burn-message.ts — 阅后即焚服务端支持
 *
 * 服务端在阅后即焚中的角色（最小化原则）：
 * 1. 存储消息时记录 burnAfterRead 和 burnExpireAt
 * 2. 定期清理过期消息（Cron 任务）
 * 3. 接收客户端的销毁通知，删除服务端副本
 * 4. 不参与倒计时逻辑（完全由客户端处理）
 *
 * 安全原则：
 * - 服务器只是辅助清理，不依赖服务器来保证消息销毁
 * - 真正的销毁在客户端完成
 * - 结合 MLS E2EE，服务器无法看到消息内容
 */

import { Router, Request, Response } from 'express';
import prisma from './db.js';
import { userAuth } from './auth.js';

const burnRouter = Router();

// ============================================================
// 1. 群消息阅后即焚
// ============================================================

/**
 * POST /api/group/burn-message
 * 客户端通知服务器销毁群消息
 *
 * ★ 鉴权要求：
 * 1. 调用者必须登录（userAuth）
 * 2. 调用者必须是该消息所在群的成员
 * 3. 该消息必须是阅后即焚消息（burnAfterRead 非空），防止滥用此接口擦除普通消息
 */
burnRouter.post('/burn-message', userAuth, async (req: Request, res: Response) => {
  const { messageId, chatId } = req.body;
  const userId = (req as any).user.id;

  if (!messageId) {
    return res.status(400).json({ error: '缺少 messageId' });
  }

  try {
    const msg = await prisma.groupMessage.findUnique({
      where: { id: messageId },
      select: { id: true, groupId: true, burnAfterRead: true, isRevoked: true },
    });
    if (!msg) {
      return res.status(404).json({ error: '消息不存在' });
    }
    if (msg.isRevoked) {
      return res.json({ ok: true, alreadyBurned: true });
    }
    // 只有群成员可以销毁该群的阅后即焚消息
    const member = await prisma.groupMember.findUnique({
      where: { groupId_userId: { groupId: msg.groupId, userId } },
      select: { id: true },
    });
    if (!member) {
      console.warn(`[Burn] 非成员 ${userId} 尝试销毁群 ${msg.groupId} 的消息 ${messageId}，已拒绝`);
      return res.status(403).json({ error: '非群成员，无权销毁该消息' });
    }
    // 只允许销毁标记为阅后即焚的消息
    if (msg.burnAfterRead == null) {
      return res.status(403).json({ error: '该消息不是阅后即焚消息' });
    }

    // 标记消息为已撤回（软删除）并清空内容
    await prisma.groupMessage.update({
      where: { id: messageId },
      data: {
        isRevoked: true,
        content: '[消息已销毁]',
        extra: null,
      },
    });

    console.log(`[Burn] 群消息 ${messageId} 已被 ${userId} 销毁`);
    res.json({ ok: true });
  } catch (err: any) {
    console.error('[Burn] 销毁群消息失败:', err.message);
    res.status(500).json({ error: '销毁失败' });
  }
});

// ============================================================
// 2. 定期清理过期消息
// ============================================================

/**
 * 清理所有过期的阅后即焚消息
 * 建议每分钟执行一次
 */
export async function cleanupExpiredBurnMessages(): Promise<number> {
  const now = new Date();
  let totalCleaned = 0;

  try {
    // 清理过期的群消息
    const groupResult = await prisma.groupMessage.updateMany({
      where: {
        burnExpireAt: {
          not: null,
          lte: now,
        },
        isRevoked: false,
      },
      data: {
        isRevoked: true,
        content: '[消息已过期销毁]',
        extra: null,
      },
    });
    totalCleaned += groupResult.count;

    // 清理过期的私聊消息
    const privateResult = await prisma.privateMessage.updateMany({
      where: {
        burnExpireAt: {
          not: null,
          lte: now,
        },
        isRevoked: false,
      },
      data: {
        isRevoked: true,
        content: '[消息已过期销毁]',
        extra: null,
      },
    });
    totalCleaned += privateResult.count;

    if (totalCleaned > 0) {
      console.log(`[Burn] 定期清理: 销毁 ${totalCleaned} 条过期消息 (群:${groupResult.count} 私:${privateResult.count})`);
    }
  } catch (err: any) {
    console.error('[Burn] 定期清理失败:', err.message);
  }

  return totalCleaned;
}

/**
 * 启动定期清理任务
 * @param intervalMs 清理间隔（毫秒），默认 60 秒
 */
export function startBurnCleanupCron(intervalMs = 60000): ReturnType<typeof setInterval> {
  console.log(`[Burn] 启动定期清理任务, 间隔 ${intervalMs / 1000}秒`);
  return setInterval(() => {
    cleanupExpiredBurnMessages().catch(err => {
      console.error('[Burn] 清理任务异常:', err);
    });
  }, intervalMs);
}

export default burnRouter;
