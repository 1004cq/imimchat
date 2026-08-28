/**
 * 私聊同步层（学 TDLib 职责，非 TDLib 协议）
 * - client_msg_id 幂等
 * - per-chat seq 游标
 * - 重连后按 afterSeq 补洞（不假设 WS 必达）
 */
import type { Message } from '@/lib/store';

const SEQ_KEY_PREFIX = 'cqim:private-seq:';

export function createClientMsgId(): string {
  return `cm-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}

/** WS tempId 与 HTTP clientMsgId 同源，供幂等去重 */
export function clientMsgIdPair(existing?: string): { tempId: string; clientMsgId: string } {
  const id = existing || createClientMsgId();
  return { tempId: id, clientMsgId: id };
}

export function getLastPrivateSeq(chatId: string): number {
  try {
    return parseInt(localStorage.getItem(`${SEQ_KEY_PREFIX}${chatId}`) || '0', 10) || 0;
  } catch {
    return 0;
  }
}

export function setLastPrivateSeq(chatId: string, seq: number): void {
  if (!chatId || !seq || seq <= 0) return;
  try {
    const prev = getLastPrivateSeq(chatId);
    if (seq > prev) {
      localStorage.setItem(`${SEQ_KEY_PREFIX}${chatId}`, String(seq));
    }
  } catch {
    // ignore quota errors
  }
}

export function trackPrivateSeqFromMessages(chatId: string, messages: Array<{ seq?: number }>): void {
  const maxSeq = messages.reduce((max, m) => Math.max(max, m.seq || 0), 0);
  if (maxSeq > 0) setLastPrivateSeq(chatId, maxSeq);
}

export interface PrivateGapSyncResult {
  ok: boolean;
  messages: Array<Record<string, unknown>>;
  notFound?: boolean;
}

/** 重连后按 seq 补拉缺口；返回原始服务端消息列表 */
export async function fetchPrivateGap(
  chatId: string,
  token: string,
  afterSeq?: number,
): Promise<PrivateGapSyncResult> {
  const cursor = afterSeq ?? getLastPrivateSeq(chatId);
  const url = `/api/chat/${chatId}/messages?afterSeq=${cursor}&limit=100`;
  const response = await fetch(url, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (response.status === 404) {
    return { ok: false, messages: [], notFound: true };
  }
  if (!response.ok) {
    return { ok: false, messages: [] };
  }
  const data = await response.json();
  const messages = Array.isArray(data?.messages) ? data.messages : [];
  trackPrivateSeqFromMessages(chatId, messages as Array<{ seq?: number }>);
  return { ok: true, messages };
}

/** 将服务端私聊消息转为客户端 Message 骨架（解密由调用方处理） */
export function mapServerPrivateRow(
  m: Record<string, any>,
  chatId: string,
  currentUserId: string,
): Message {
  return {
    id: m.id,
    chatId,
    cursor: m.id,
    seq: m.seq,
    senderId: m.senderId,
    content: m.isRevoked ? '消息已撤回' : (m.content || ''),
    type: (m.msgType || 'text') as Message['type'],
    timestamp: m.createdAt || Date.now(),
    isEncrypted: m.msgType === 'encrypted',
    reactions: {},
    status: m.status || 'sent',
    isRecalled: m.isRevoked || false,
    replyTo: m.replyToId || undefined,
    direction: m.senderId === currentUserId ? 'outbound' : 'inbound',
    ...(m.burnAfterRead ? { burnAfterRead: m.burnAfterRead } : {}),
    ...(m.hmac ? { hmac: m.hmac, integrityStatus: 'unverified' as const } : {}),
  };
}
