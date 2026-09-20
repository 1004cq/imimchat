import type { Message } from '@/lib/store';

export const DISPLAY_MESSAGE_TYPES = [
  'text',
  'image',
  'video',
  'file',
  'voice',
  'system',
  'call',
  'location',
  'location_share',
  'sticker',
] as const;

export type DisplayMessageType = (typeof DISPLAY_MESSAGE_TYPES)[number];

const DISPLAY_TYPE_SET = new Set<string>(DISPLAY_MESSAGE_TYPES);
const ENCRYPTED_WIRE_TYPES = new Set(['encrypted', 'mls_encrypted']);

const OWN_CIPHERTEXT_PLACEHOLDER = '🔒 [本地加密消息]';
const HISTORY_DECRYPT_PLACEHOLDER = '🔒 无法解密历史消息，请重新验证安全会话';
const LIVE_DECRYPT_PLACEHOLDER = '🔒 无法解密消息，请重置安全会话';
const LEGACY_PLAINTEXT_PLACEHOLDER = '⚠️ [不支持的旧明文消息]';

export function coerceTimestamp(value: unknown, fallback = Date.now()): number {
  if (typeof value === 'number' && Number.isFinite(value) && value > 0) return value;
  if (typeof value === 'string' && value.trim()) {
    const asNumber = Number(value);
    if (Number.isFinite(asNumber) && asNumber > 0) return asNumber;
    const parsed = Date.parse(value);
    if (Number.isFinite(parsed) && parsed > 0) return parsed;
  }
  if (value instanceof Date) {
    const ms = value.getTime();
    if (Number.isFinite(ms) && ms > 0) return ms;
  }
  return fallback;
}

export function coerceDisplayMessageType(type: unknown): DisplayMessageType {
  if (typeof type === 'string' && DISPLAY_TYPE_SET.has(type)) return type as DisplayMessageType;
  return 'text';
}

export function parseJsonObject(value: unknown): Record<string, any> {
  if (!value) return {};
  if (typeof value === 'string') {
    try {
      const parsed = JSON.parse(value);
      return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : {};
    } catch {
      return {};
    }
  }
  if (typeof value === 'object' && !Array.isArray(value)) return value as Record<string, any>;
  return {};
}

export function parseDecryptedInnerPayload(plaintext: string): {
  content: string;
  msgType: DisplayMessageType;
  extra: Record<string, any>;
} {
  const parsed = JSON.parse(plaintext);
  if (typeof parsed === 'string') {
    return { content: parsed, msgType: 'text', extra: {} };
  }
  if (!parsed || typeof parsed !== 'object') {
    throw new Error('invalid_plaintext');
  }
  const content = typeof parsed.content === 'string'
    ? parsed.content
    : parsed.content == null
      ? ''
      : String(parsed.content);
  return {
    content,
    msgType: coerceDisplayMessageType(parsed.msgType),
    extra: parseJsonObject(parsed.extra),
  };
}

export type PrivateDecryptResult = { plaintext?: string; error?: string; success: boolean };

export function resolvePrivateWireMessage(input: {
  msgType?: string;
  content?: string;
  isRevoked?: boolean;
  isOwn: boolean;
  extra?: unknown;
  decryptResult?: PrivateDecryptResult;
  failedPlaceholder?: string;
}): {
  content: string;
  type: DisplayMessageType;
  extra: Record<string, any>;
  decryptionFailed: boolean;
  decryptionStatus: NonNullable<Message['decryptionStatus']>;
  isEncrypted: boolean;
} {
  const wireType = typeof input.msgType === 'string' ? input.msgType : 'text';
  const isEncryptedWire = ENCRYPTED_WIRE_TYPES.has(wireType);
  const extra = parseJsonObject(input.extra);

  if (input.isRevoked) {
    return {
      content: '消息已撤回',
      type: 'text',
      extra,
      decryptionFailed: false,
      decryptionStatus: isEncryptedWire ? 'decrypted' : 'legacy',
      isEncrypted: isEncryptedWire,
    };
  }

  if (!isEncryptedWire) {
    return {
      content: LEGACY_PLAINTEXT_PLACEHOLDER,
      type: 'text',
      extra,
      decryptionFailed: true,
      decryptionStatus: 'legacy',
      isEncrypted: false,
    };
  }

  if (input.isOwn) {
    return {
      content: OWN_CIPHERTEXT_PLACEHOLDER,
      type: 'text',
      extra,
      decryptionFailed: false,
      decryptionStatus: 'ciphertext',
      isEncrypted: true,
    };
  }

  const result = input.decryptResult;
  try {
    if (!result?.success || !result.plaintext) throw new Error(result?.error || 'decrypt_failed');
    const inner = parseDecryptedInnerPayload(result.plaintext);
    return {
      content: inner.content,
      type: inner.msgType,
      extra: { ...extra, ...inner.extra },
      decryptionFailed: false,
      decryptionStatus: 'decrypted',
      isEncrypted: true,
    };
  } catch {
    return {
      content: input.failedPlaceholder || LIVE_DECRYPT_PLACEHOLDER,
      type: 'text',
      extra,
      decryptionFailed: true,
      decryptionStatus: 'failed',
      isEncrypted: true,
    };
  }
}

export function failedDecryptResults<T extends { id: string }>(
  messages: T[],
  error: unknown,
): Array<{ id: string; success: false; error: string }> {
  const message = error instanceof Error ? error.message : String(error);
  return messages.map(item => ({ id: item.id, success: false as const, error: message }));
}

/** 保证消息列表始终是可安全 map / 渲染的数组 */
export function sanitizeMessages(list: unknown): Message[] {
  if (!Array.isArray(list)) return [];
  return list
    .filter((item): item is Message => {
      if (!item || typeof item !== 'object') return false;
      const msg = item as Message;
      return typeof msg.id === 'string' && msg.id.length > 0 && typeof msg.senderId === 'string';
    })
    .map(msg => {
      const content = typeof msg.content === 'string' ? msg.content : (msg.content == null ? '' : String(msg.content));
      const decryptedContent = typeof msg.decryptedContent === 'string'
        ? msg.decryptedContent
        : msg.decryptedContent == null
          ? undefined
          : String(msg.decryptedContent);
      return {
        ...msg,
        reactions: msg.reactions && typeof msg.reactions === 'object' && !Array.isArray(msg.reactions)
          ? msg.reactions
          : {},
        content,
        decryptedContent,
        type: coerceDisplayMessageType(msg.type),
        timestamp: coerceTimestamp(msg.timestamp),
      };
    });
}

export { OWN_CIPHERTEXT_PLACEHOLDER, HISTORY_DECRYPT_PLACEHOLDER, LIVE_DECRYPT_PLACEHOLDER, LEGACY_PLAINTEXT_PLACEHOLDER };
