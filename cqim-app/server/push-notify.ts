/**
 * 离线推送主路径：自建 APNs（P8）与 Web Push（VAPID）。
 * 消息密文仍只存 DB；可选的 pushPreview 仅用于系统通知正文，不落库。
 */
import prisma from './db.js';
import { sendAPNsPush } from './apns.js';
import { sendWebPush } from './web-push.js';
import { shouldSkipApns } from './presence.js';
import { fallbackPushBody } from './push-preview.js';

export interface PrivatePushParams {
  toUserId: string;
  senderId: string;
  chatId: string;
  messageId: string;
  /** Ephemeral lock-screen preview; never written to PrivateMessage */
  previewText?: string;
}

export interface GroupPushParams {
  toUserId: string;
  groupId: string;
  senderId: string;
  senderName: string;
  previewText?: string;
}

async function loadSenderLabel(senderId: string): Promise<string> {
  try {
    const sender = await prisma.user.findUnique({
      where: { id: senderId },
      select: { nickname: true, username: true },
    });
    return sender?.nickname || sender?.username || 'IMIM';
  } catch {
    return 'IMIM';
  }
}

export async function notifyPrivateMessagePush(params: PrivatePushParams): Promise<void> {
  const { toUserId, senderId, chatId, messageId, previewText } = params;
  if (await shouldSkipApns(toUserId, chatId)) return;

  const title = await loadSenderLabel(senderId);
  const body = fallbackPushBody(previewText);

  await sendAPNsPush({
    toUserId,
    title,
    body,
    customData: { chatId, messageId, senderId, type: 'message' },
  }).catch((error: unknown) => console.error('[APNs] 私聊推送异常:', error));

  await sendWebPush({ toUserId, chatId, messageId, senderId, title, body })
    .catch((error: unknown) => console.error('[WebPush] 私聊推送异常:', error));
}

export async function notifyGroupMessagePush(params: GroupPushParams): Promise<void> {
  const { toUserId, groupId, senderId, senderName, previewText } = params;
  if (await shouldSkipApns(toUserId)) return;

  const title = senderName || '群聊';
  const body = fallbackPushBody(previewText);

  await sendAPNsPush({
    toUserId,
    title,
    body,
    customData: { groupId, senderId, type: 'group_message' },
  }).catch((error: unknown) => console.error('[APNs] 群聊推送异常:', error));

  await sendWebPush({ toUserId, chatId: groupId, senderId, title, body })
    .catch((error: unknown) => console.error('[WebPush] 群聊推送异常:', error));
}
