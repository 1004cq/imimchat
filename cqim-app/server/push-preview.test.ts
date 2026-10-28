import { describe, expect, it } from 'vitest';
import { fallbackPushBody, looksLikePushCiphertext, sanitizePushPreview } from './push-preview.ts';

describe('sanitizePushPreview', () => {
  it('keeps short plaintext', () => {
    expect(sanitizePushPreview('你好')).toBe('你好');
  });

  it('truncates long text', () => {
    const long = '啊'.repeat(80);
    const out = sanitizePushPreview(long);
    expect(out?.endsWith('…')).toBe(true);
    expect(out!.length).toBe(73);
  });

  it('rejects ciphertext envelopes', () => {
    expect(sanitizePushPreview('{"ct":"abc","ratchetKey":"x"}')).toBeNull();
    expect(sanitizePushPreview('🔒 [加密消息]')).toBeNull();
  });
});

describe('fallbackPushBody', () => {
  it('prefers sanitized preview', () => {
    expect(fallbackPushBody('在吗')).toBe('在吗');
  });

  it('falls back when preview opaque', () => {
    expect(fallbackPushBody('🔒 [加密消息]')).toBe('发来一条消息');
    expect(looksLikePushCiphertext('🔒 x')).toBe(true);
  });
});
