/**
 * 统一离线推送。仅 presence=foreground 时跳过，不要因有 WS 就 skip。
 */
import prisma from './db.js';
import { avatarToProxy } from './cos-signer.js';
import { publicUrl } from './public-url.js';
import { sendWebPush } from './web-push.js';
import { shouldSkipApns } from './presence.js';

export interface PrivatePushParams {
  toUserId: string;
  senderId: string;
  chatId: string;
  messageId: string;
  previewText?: string;
}

export interface GroupPushParams {
  toUserId: string;
  groupId: string;
  senderId: string;
  senderName: string;
  previewText?: string;
}

async function loadSender(senderId: string) {
  return prisma.user.findUnique({
    where: { id: senderId },
    select: { nickname: true, username: true, avatar: true },
  });
}

async function loadPeerToken(userId: string) {
  return prisma.user.findUnique({
    where: { id: userId },
    select: { fcmToken: true },
  });
}

export async function notifyPrivateMessagePush(params: PrivatePushParams): Promise<void> {
  const { toUserId, senderId, chatId, messageId, previewText = '🔒 [加密消息]' } = params;

  if (await shouldSkipApns(toUserId, chatId)) {
    console.log('[push] skip APNs, peer foreground', toUserId);
    return;
  }

  const [senderUser, peerUser] = await Promise.all([
    loadSender(senderId),
    loadPeerToken(toUserId),
  ]);

  const senderName = senderUser?.nickname || senderUser?.username || '有人';
  const senderAvatarPath = avatarToProxy(senderUser?.avatar);
  const senderAvatarUrl = senderAvatarPath ? publicUrl(senderAvatarPath) : '';

  const { parseAPNsToken, sendAPNsPush } = await import('./apns.js');
  if (parseAPNsToken(peerUser?.fcmToken)) {
    await sendAPNsPush({
      toUserId,
      title: senderName,
      body: previewText,
      senderAvatar: senderAvatarUrl,
      customData: { chatId, senderId, sender_name: senderName },
    }).catch((e: unknown) => console.error('[APNs] 推送异常:', e));
  } else {
    const { parseJPushToken, sendJPushPush } = await import('./jpush.js');
    if (parseJPushToken(peerUser?.fcmToken)) {
      await sendJPushPush({
        toUserId,
        title: senderName,
        body: previewText,
        extras: { chatId, senderId },
      }).catch((e: unknown) => console.error('[JPush] 推送异常:', e));
    } else {
      const { parseGetuiToken, sendGetuiPush } = await import('./getui.js');
      if (parseGetuiToken(peerUser?.fcmToken)) {
        await sendGetuiPush({
          toUserId,
          title: senderName,
          body: previewText,
          senderAvatar: senderAvatarUrl,
          payload: JSON.stringify({ chatId, senderId, senderAvatar: senderAvatarUrl }),
        }).catch((e: unknown) => console.error('[个推] 推送异常:', e));
      } else if (peerUser?.fcmToken) {
        const { sendFCMPush } = await import('./fcm.js');
        await sendFCMPush({
          toUserId,
          title: senderName,
          body: previewText,
          data: { chatId, senderId, sender_avatar: senderAvatarUrl },
        }).catch((e: unknown) => console.error('[FCM] 推送异常:', e));
      }
    }
  }

  await sendWebPush({
    toUserId,
    chatId,
    messageId,
    senderId,
  }).catch((e: unknown) => console.error('[WebPush] 推送异常:', e));
}

export async function notifyGroupMessagePush(params: GroupPushParams): Promise<void> {
  const { toUserId, groupId, senderId, senderName, previewText = '🔒 [群消息]' } = params;
  if (await shouldSkipApns(toUserId)) return;

  const peerUser = await loadPeerToken(toUserId);
  const title = senderName || '群消息';
  const body = previewText;

  const { parseAPNsToken, sendAPNsPush } = await import('./apns.js');
  if (parseAPNsToken(peerUser?.fcmToken)) {
    await sendAPNsPush({
      toUserId,
      title,
      body,
      customData: { groupId, senderId, type: 'group_message' },
    }).catch((e: unknown) => console.error('[APNs] 群推送异常:', e));
  }
}
