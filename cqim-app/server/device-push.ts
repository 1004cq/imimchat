import { Router, Request, Response } from 'express';
import prisma from './db.js';
import { userAuth } from './auth.js';
import { setPresence } from './presence.js';

const router = Router();
router.use(userAuth);

function isAPNsToken(token: unknown): token is string {
  return typeof token === 'string' && /^[a-f0-9]{64}$/i.test(token);
}

router.post('/push-token', async (req: Request, res: Response) => {
  const currentUser = (req as any).user;
  const { token, platform, env, kind, appVersion, bundleId } = req.body || {};
  if (!isAPNsToken(token) || platform !== 'ios' || !['sandbox', 'production'].includes(env) || kind !== 'alert' || bundleId !== 'com.imim.chat') {
    return res.status(400).json({ error: '无效的 iOS APNs alert token' });
  }

  try {
    await prisma.pushDeviceToken.upsert({
      where: { token_kind: { token: token.toLowerCase(), kind } },
      create: {
        userId: currentUser.id,
        token: token.toLowerCase(),
        platform,
        environment: env,
        kind,
        appVersion: typeof appVersion === 'string' ? appVersion : null,
      },
      update: {
        userId: currentUser.id,
        platform,
        environment: env,
        appVersion: typeof appVersion === 'string' ? appVersion : null,
      },
    });

    // Retain the legacy field for an existing deployment rollback path.
    await prisma.user.update({ where: { id: currentUser.id }, data: { fcmToken: `apns:${token.toLowerCase()}` } });
    console.log(`[Push] registered iOS alert token: userId=${currentUser.id} env=${env}`);
    return res.json({ success: true });
  } catch (error) {
    console.error('[Push] token registration failed:', error);
    return res.status(500).json({ error: '推送 token 注册失败' });
  }
});

router.delete('/push-token', async (req: Request, res: Response) => {
  const currentUser = (req as any).user;
  const token = typeof req.body?.token === 'string' ? req.body.token.toLowerCase() : null;
  try {
    await prisma.$transaction([
      prisma.pushDeviceToken.deleteMany({ where: { userId: currentUser.id, ...(token ? { token } : {}) } }),
      prisma.user.update({
        where: { id: currentUser.id },
        data: { fcmToken: null, voipToken: null },
      }),
    ]);
    console.log(`[Push] removed iOS alert token: userId=${currentUser.id}`);
    return res.json({ success: true });
  } catch (error) {
    console.error('[Push] token removal failed:', error);
    return res.status(500).json({ error: '推送 token 删除失败' });
  }
});

router.post('/presence', async (req: Request, res: Response) => {
  const currentUser = (req as any).user;
  const { presence, activeChatId } = req.body || {};
  try {
    if (presence === 'offline') {
      await setPresence(currentUser.id, 'offline');
    } else if (presence === 'foreground' || presence === 'background') {
      await setPresence(currentUser.id, presence, typeof activeChatId === 'string' ? activeChatId : null);
    } else {
      return res.status(400).json({ error: '无效的 presence' });
    }
    return res.json({ success: true });
  } catch (error) {
    console.error('[Push] presence update failed:', error);
    return res.status(500).json({ error: '在线状态更新失败' });
  }
});

export default router;
