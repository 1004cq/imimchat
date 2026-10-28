/**
 * Ephemeral lock-screen / Web Push preview helpers.
 * Preview text is never persisted to PrivateMessage.content (E2EE envelope only).
 */

const MAX_PUSH_PREVIEW_CHARS = 72;

export function looksLikePushCiphertext(text: string): boolean {
  const value = text.trim();
  if (!value) return false;
  if (value.startsWith('{') && (
    value.includes('"ct"')
    || value.includes('ciphertext')
    || value.includes('"_mls"')
    || value.includes('"ratchetKey"')
    || value.includes('"iv"')
  )) {
    return true;
  }
  if (value.startsWith('🔒')) return true;
  return value.length > 96 && /^[A-Za-z0-9+/_=-]+$/.test(value);
}

/** Strip/truncate client-supplied pushPreview for APNs / Web notification body. */
export function sanitizePushPreview(raw: unknown): string | null {
  if (typeof raw !== 'string') return null;
  const value = raw.replace(/\s+/g, ' ').trim();
  if (!value || looksLikePushCiphertext(value)) return null;
  return value.length > MAX_PUSH_PREVIEW_CHARS
    ? `${value.slice(0, MAX_PUSH_PREVIEW_CHARS)}…`
    : value;
}

export function fallbackPushBody(previewText?: string | null): string {
  const cleaned = sanitizePushPreview(previewText || '');
  return cleaned || '发来一条消息';
}
