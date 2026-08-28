/**
 * 私聊消息唯一写库出口：先落库再推送，支持 clientMsgId 幂等与 per-chat seq。
 */
import prisma from './db.js';
import {
  incrUnreadCount,
  invalidateConversationList,
  isUserOnline,
  redis,
} from './redis.js';
import { notifyPrivateMessagePush } from './push-notify.js';
import { publishImPush } from './publish-im.js';

function parseExtra(extra: string | null): Record<string, any> | undefined {
  if (!extra) return undefined;
  try {
    return JSON.parse(extra);
  } catch {
    return undefined;
  }
}

function messagePreview(msgType: string, content: string): string {
  if (msgType === 'encrypted') return '🔒 [加密消息]';
  if (msgType === 'text') return content.slice(0, 100);
  if (msgType === 'image') return '[图片]';
  if (msgType === 'voice') return '[语音消息]';
  if (msgType === 'video') return '[视频]';
  if (msgType === 'file') return '[文件]';
  if (msgType === 'sticker') return '[贴纸]';
  if (msgType === 'location') return '[位置]';
  if (msgType === 'location_share') return '[位置共享]';
  if (msgType === 'call') return '[通话]';
  return content.slice(0, 100);
}

export interface SendPrivateMessageInput {
  chatId: string;
  senderId: string;
  msgType: string;
  content: string;
  replyToId?: string | null;
  extra?: Record<string, any>;
  burnAfterRead?: number | null;
  hmac?: string | null;
  /** client_msg_id，与 WS tempId 同义 */
  clientMsgId?: string | null;
  tempId?: string | null;
}

export interface PrivateMessagePayload {
  id: string;
  chatId: string;
  senderId: string;
  msgType: string;
  content: string;
  replyToId: string | null;
  isRevoked: boolean;
  status: string;
  extra?: Record<string, any>;
  createdAt: number;
  seq: number;
  clientMsgId?: string;
  tempId?: string;
  burnAfterRead?: number;
  hmac?: string;
}

export interface SendPrivateMessageResult {
  message: PrivateMessagePayload;
  peerId: string;
  duplicate: boolean;
}

async function nextChatSeq(chatId: string): Promise<bigint> {
  const max = await prisma.privateMessage.aggregate({
    where: { chatId },
    _max: { seq: true },
  });
  return (max._max.seq ?? 0n) + 1n;
}

export function formatPrivateMessage(
  message: {
    id: string;
    chatId: string;
    senderId: string;
    msgType: string;
    content: string;
    replyToId: string | null;
    isRevoked: boolean;
    status: string;
    extra: string | null;
    createdAt: Date;
    seq: bigint;
    clientMsgId: string | null;
    burnAfterRead: number | null;
    hmac: string | null;
  },
  opts?: { tempId?: string },
): PrivateMessagePayload {
  const payload: PrivateMessagePayload = {
    id: message.id,
    chatId: message.chatId,
    senderId: message.senderId,
    msgType: message.msgType,
    content: message.isRevoked ? '消息已撤回' : message.content,
    replyToId: message.replyToId,
    isRevoked: message.isRevoked,
    status: message.status,
    extra: message.isRevoked ? undefined : parseExtra(message.extra),
    createdAt: message.createdAt.getTime(),
    seq: Number(message.seq),
    ...(message.clientMsgId ? { clientMsgId: message.clientMsgId } : {}),
    ...(opts?.tempId ? { tempId: opts.tempId } : {}),
    ...(message.burnAfterRead ? { burnAfterRead: message.burnAfterRead } : {}),
    ...(message.hmac ? { hmac: message.hmac } : {}),
  };
  return payload;
}

/** 先写库，再推送；调用方负责 HTTP 响应或 WS ACK */
export async function sendPrivateMessageCore(
  input: SendPrivateMessageInput,
): Promise<SendPrivateMessageResult> {
  const dedupeId = input.clientMsgId || input.tempId || null;

  const chat = await prisma.chat.findUnique({ where: { id: input.chatId } });
  if (!chat) {
    throw Object.assign(new Error('会话不存在'), { statusCode: 404 });
  }
  if (chat.participantA !== input.senderId && chat.participantB !== input.senderId) {
    throw Object.assign(new Error('无权发送消息'), { statusCode: 403 });
  }

  if (dedupeId) {
    const existing = await prisma.privateMessage.findFirst({
      where: { chatId: input.chatId, clientMsgId: dedupeId },
    });
    if (existing) {
      const peerId = chat.participantA === input.senderId ? chat.participantB : chat.participantA;
      return {
        message: formatPrivateMessage(existing, { tempId: dedupeId }),
        peerId,
        duplicate: true,
      };
    }
  }

  const validBurnTimers = [5, 10, 30, 60, 300, 3600, 86400, 604800];
  const burnSeconds = (typeof input.burnAfterRead === 'number' && validBurnTimers.includes(input.burnAfterRead))
    ? input.burnAfterRead
    : null;

  const seq = await nextChatSeq(input.chatId);

  const message = await prisma.privateMessage.create({
    data: {
      chatId: input.chatId,
      senderId: input.senderId,
      msgType: input.msgType,
      content: input.content || '',
      replyToId: input.replyToId || null,
      extra: input.extra ? JSON.stringify(input.extra) : null,
      status: 'sent',
      burnAfterRead: burnSeconds,
      hmac: (typeof input.hmac === 'string' && /^[a-f0-9]{64}$/i.test(input.hmac)) ? input.hmac : null,
      seq,
      clientMsgId: dedupeId,
    },
  });

  await prisma.chat.update({
    where: { id: input.chatId },
    data: {
      lastMessage: messagePreview(input.msgType, input.content),
      lastMessageAt: message.createdAt,
    },
  });

  const peerId = chat.participantA === input.senderId ? chat.participantB : chat.participantA;
  return {
    message: formatPrivateMessage(message, { tempId: dedupeId || undefined }),
    peerId,
    duplicate: false,
  };
}

export async function deliverPrivateMessage(
  appLocals: {
    trySendTo?: (userId: string, msg: Record<string, any>) => boolean;
    sendTo?: (userId: string, msg: Record<string, any>) => void;
  },
  senderId: string,
  peerId: string,
  chatId: string,
  payload: PrivateMessagePayload,
  options?: { skipUnread?: boolean; skipPushNotify?: boolean },
): Promise<boolean> {
  if (!options?.skipUnread) {
    await incrUnreadCount(peerId, chatId, 1);
    await invalidateConversationList(senderId);
    await invalidateConversationList(peerId);
  }

  const wsPayload = { type: 'private_message', payload };
  const delivered = appLocals.trySendTo
    ? appLocals.trySendTo(peerId, wsPayload)
    : Boolean(appLocals.sendTo && (appLocals.sendTo(peerId, wsPayload), true));

  if (!delivered) {
    await publishImPush(peerId, wsPayload);
  }

  if (!delivered && !options?.skipPushNotify) {
    const peerOnlineElsewhere = await isUserOnline(peerId).catch(() => false)
      || Boolean(await redis.get(`user:online:${peerId}`).catch(() => null));
    if (!peerOnlineElsewhere) {
      void notifyPrivateMessagePush({
        toUserId: peerId,
        senderId,
        chatId,
        messageId: payload.id,
        previewText: messagePreview(payload.msgType, payload.content),
      });
    }
  }

  return delivered;
}
