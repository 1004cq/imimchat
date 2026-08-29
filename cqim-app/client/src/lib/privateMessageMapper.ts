import type { Message } from '@/lib/store';

export function parseMessageExtra(extra: unknown): Record<string, unknown> {
  if (!extra) return {};
  if (typeof extra === 'object') return extra as Record<string, unknown>;
  if (typeof extra !== 'string') return {};
  try {
    const parsed = JSON.parse(extra);
    return parsed && typeof parsed === 'object' ? parsed as Record<string, unknown> : {};
  } catch {
    return {};
  }
}

/** 保证气泡渲染拿到的 content 始终是 string */
export function coerceMessageContent(value: unknown): string {
  if (value == null) return '';
  if (typeof value === 'string') return value;
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  try {
    return JSON.stringify(value);
  } catch {
    return '[无法显示的消息内容]';
  }
}

export function applyMessageExtraFields(extra: Record<string, unknown>): Partial<Message> {
  const out: Partial<Message> = {};
  if (typeof extra.voiceUrl === 'string') {
    out.voiceUrl = extra.voiceUrl;
    out.duration = typeof extra.duration === 'number' ? extra.duration : 0;
  }
  if (typeof extra.voiceCiphertext === 'string') {
    out.voiceCiphertext = extra.voiceCiphertext;
    out.voiceIv = typeof extra.voiceIv === 'string' ? extra.voiceIv : undefined;
    out.voiceKeyBase64 = typeof extra.voiceKeyBase64 === 'string' ? extra.voiceKeyBase64 : undefined;
    out.voiceMimeType = typeof extra.voiceMimeType === 'string' ? extra.voiceMimeType : undefined;
    out.voiceWaveform = Array.isArray(extra.voiceWaveform) ? extra.voiceWaveform as number[] : undefined;
    out.duration = typeof extra.duration === 'number' ? extra.duration : out.duration || 0;
  }
  if (typeof extra.imageUrl === 'string') out.imageUrl = extra.imageUrl;
  if (typeof extra.videoUrl === 'string') out.videoUrl = extra.videoUrl;
  if (extra.locationData && typeof extra.locationData === 'object') {
    out.locationData = extra.locationData as Message['locationData'];
  }
  if (typeof extra.stickerUrl === 'string') {
    out.stickerUrl = extra.stickerUrl;
    out.stickerEmoji = typeof extra.stickerEmoji === 'string' ? extra.stickerEmoji : undefined;
    out.stickerSetName = typeof extra.stickerSetName === 'string' ? extra.stickerSetName : undefined;
  }
  return out;
}

export interface DecryptResult {
  id: string;
  plaintext?: string;
  error?: string;
  success: boolean;
}

export function mapServerPrivateRow(
  m: Record<string, unknown>,
  chatId: string,
  currentUserId: string,
  decryptResults?: Map<string, DecryptResult>,
): Message {
  const msgType = typeof m.msgType === 'string' ? m.msgType : 'text';
  let decryptedContent = m.isRevoked ? '消息已撤回' : coerceMessageContent(m.content);
  let finalMsgType = msgType;
  let finalExtra = parseMessageExtra(m.extra);
  let decryptionFailed = false;
  let decryptionStatus: Message['decryptionStatus'] = 'decrypted';

  if (msgType === 'encrypted' && m.content && !m.isRevoked) {
    if (m.senderId === currentUserId) {
      decryptedContent = '🔒 [本地加密消息]';
      decryptionStatus = 'ciphertext';
    } else {
      const result = decryptResults?.get(String(m.id));
      try {
        if (!result?.success || !result.plaintext) throw new Error(result?.error || 'decrypt_failed');
        const decrypted = JSON.parse(result.plaintext) as { content?: unknown; msgType?: string; extra?: Record<string, unknown> };
        decryptedContent = coerceMessageContent(decrypted.content);
        finalMsgType = decrypted.msgType || 'text';
        finalExtra = { ...finalExtra, ...(decrypted.extra || {}) };
      } catch {
        decryptedContent = '🔒 无法解密历史消息，请重新验证安全会话';
        decryptionFailed = true;
        decryptionStatus = 'failed';
      }
    }
  } else if (msgType === 'text' && !m.isRevoked) {
    // 迁移前旧明文；保留原文展示，仅标记 legacy
    decryptedContent = coerceMessageContent(m.content);
    decryptionFailed = true;
    decryptionStatus = 'legacy';
  } else if (!m.isRevoked) {
    // 语音/贴纸/位置等非 E2EE 结构化消息：保留服务端 content + extra
    decryptedContent = coerceMessageContent(m.content);
    decryptionStatus = 'legacy';
  }

  return {
    id: String(m.id),
    chatId: typeof m.chatId === 'string' ? m.chatId : chatId,
    cursor: String(m.id),
    seq: typeof m.seq === 'number' ? m.seq : undefined,
    senderId: String(m.senderId || ''),
    content: decryptedContent,
    type: finalMsgType as Message['type'],
    timestamp: typeof m.createdAt === 'number' ? m.createdAt : Date.now(),
    isEncrypted: msgType === 'encrypted',
    decryptionFailed,
    decryptionStatus,
    direction: m.senderId === currentUserId ? 'outbound' : 'inbound',
    reactions: {},
    status: (typeof m.status === 'string' ? m.status : 'sent') as Message['status'],
    isRecalled: Boolean(m.isRevoked),
    replyTo: typeof m.replyToId === 'string' ? m.replyToId : undefined,
    ...applyMessageExtraFields(finalExtra),
    ...(typeof m.hmac === 'string' ? { hmac: m.hmac, integrityStatus: 'unverified' as const } : {}),
  };
}
