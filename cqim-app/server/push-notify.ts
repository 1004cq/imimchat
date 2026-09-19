/**
 * 离线推送主路径：自建 APNs（P8）与 Web Push（VAPID）。
 * 推送 payload 只携带路由 ID，不携带消息明文。
 */
import prisma from './db.js';
import { sendAPNsPush } from './apns.js';
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
    select: { nickname: true, username: true },
  });
}

export async function notifyPrivateMessagePush(params: PrivatePushParams): Promise<void> {
  const { toUserId, senderId, chatId, messageId } = params;
  if (await shouldSkipApns(toUserId, chatId)) return;

  const sender = await loadSender(senderId);
  const conversationName = sender?.nickname || sender?.username || '有人';

  await sendAPNsPush({
    toUserId,
    title: '新消息',
    body: conversationName,
    customData: { chatId, senderId },
  }).catch((error: unknown) => console.error('[APNs] 私聊推送异常:', error));

  await sendWebPush({ toUserId, chatId, messageId, senderId })
    .catch((error: unknown) => console.error('[WebPush] 私聊推送异常:', error));
}

export async function notifyGroupMessagePush(params: GroupPushParams): Promise<void> {
  const { toUserId, groupId, senderId, senderName } = params;
  if (await shouldSkipApns(toUserId)) return;

  await sendAPNsPush({
    toUserId,
    title: '新消息',
    body: senderName || '群聊消息',
    customData: { groupId, senderId, type: 'group_message' },
  }).catch((error: unknown) => console.error('[APNs] 群聊推送异常:', error));

  await sendWebPush({ toUserId, chatId: groupId, senderId })
    .catch((error: unknown) => console.error('[WebPush] 群聊推送异常:', error));
}
