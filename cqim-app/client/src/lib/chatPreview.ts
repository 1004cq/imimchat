/**
 * Chat list / bubble preview helpers.
 * Server lastMessage is ciphertext-safe ("🔒 [加密消息]"); the client may
 * have a decrypted local copy and must never render raw envelopes.
 */

const OPAQUE_PREVIEWS = new Set([
  '🔒 [加密消息]',
  '🔒 [本地加密消息]',
  '🔒 [群消息]',
  '🔒 加密消息（无法解密，请等待群安全会话同步）',
  '🔒 无法解密历史消息，请重新验证安全会话',
  '🔒 无法解密消息，请重置安全会话',
  '⚠️ [不支持的旧明文消息]',
  '⚠️ [不支持的旧明文群消息]',
]);

export function looksLikeCiphertext(text?: string | null): boolean {
  if (typeof text !== 'string') return false;
  const value = text.trim();
  if (!value) return false;
  if (value.startsWith('{') && (
    value.includes('"ct"')
    || value.includes('ciphertext')
    || value.includes('"_mls"')
    || value.includes('"iv"')
    || value.includes('"ratchetKey"')
  )) {
    return true;
  }
  return value.length > 96 && /^[A-Za-z0-9+/_=-]+$/.test(value);
}

export function isOpaquePreview(text?: string | null): boolean {
  const value = (text || '').trim();
  if (!value) return true;
  if (OPAQUE_PREVIEWS.has(value)) return true;
  if (value.startsWith('🔒') && (value.includes('无法解密') || value.includes('加密消息'))) return true;
  return looksLikeCiphertext(value);
}

export function formatMediaPreview(type?: string): string | null {
  switch (type) {
    case 'image': return '[图片]';
    case 'video': return '[视频]';
    case 'voice': return '[语音]';
    case 'sticker': return '[贴纸]';
    case 'location':
    case 'location_share': return '[位置]';
    case 'file': return '[文件]';
    case 'call': return '[通话]';
    default: return null;
  }
}

export function sanitizePreviewText(text?: string | null): string {
  const value = (text || '').replace(/\s+/g, ' ').trim();
  if (!value) return '';
  if (looksLikeCiphertext(value)) return '🔒 [加密消息]';
  return value.slice(0, 100);
}

export function formatChatListPreview(input: {
  content?: string;
  type?: string;
  decryptionStatus?: string;
  isRecalled?: boolean;
  stickerEmoji?: string;
}): string {
  if (input.isRecalled) return '消息已撤回';
  if (input.decryptionStatus === 'failed') return '🔒 无法解密';
  if (input.decryptionStatus === 'ciphertext') return '🔒 [加密消息]';
  if (input.type === 'sticker') {
    return input.stickerEmoji ? `[贴纸] ${input.stickerEmoji}` : '[贴纸]';
  }
  const media = formatMediaPreview(input.type);
  if (media) return media;
  const content = sanitizePreviewText(input.content);
  if (!content) return '';
  if (isOpaquePreview(content) && input.decryptionStatus !== 'decrypted') {
    return '🔒 [加密消息]';
  }
  return content;
}

/** Prefer a locally decrypted preview over the server lock/ciphertext placeholder. */
export function preferLocalChatPreview(incoming?: string | null, existing?: string | null): string {
  const incomingClean = sanitizePreviewText(incoming);
  const existingClean = sanitizePreviewText(existing);
  if (isOpaquePreview(incoming) && existingClean && !isOpaquePreview(existing)) {
    return existingClean;
  }
  return incomingClean || existingClean || '';
}
