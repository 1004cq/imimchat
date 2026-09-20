import { describe, expect, it } from 'vitest';
import { looksLikeCiphertext } from './chatPreview';
import {
  coerceDisplayMessageType,
  coerceTimestamp,
  failedDecryptResults,
  HISTORY_DECRYPT_PLACEHOLDER,
  parseDecryptedInnerPayload,
  resolvePrivateWireMessage,
  sanitizeMessages,
} from './messageListUtils';

describe('coerceDisplayMessageType', () => {
  it('keeps known bubble types', () => {
    expect(coerceDisplayMessageType('image')).toBe('image');
    expect(coerceDisplayMessageType('sticker')).toBe('sticker');
    expect(coerceDisplayMessageType('text')).toBe('text');
  });

  it('maps wire ciphertext types and unknown values to text', () => {
    expect(coerceDisplayMessageType('encrypted')).toBe('text');
    expect(coerceDisplayMessageType('mls_encrypted')).toBe('text');
    expect(coerceDisplayMessageType('prekey')).toBe('text');
    expect(coerceDisplayMessageType(undefined)).toBe('text');
  });
});

describe('coerceTimestamp', () => {
  it('parses ISO strings and numeric strings instead of replacing with Date.now()', () => {
    expect(coerceTimestamp('1710000000000')).toBe(1710000000000);
    expect(coerceTimestamp('2024-03-09T12:00:00.000Z')).toBe(Date.parse('2024-03-09T12:00:00.000Z'));
    expect(coerceTimestamp(new Date(1710000000000))).toBe(1710000000000);
  });

  it('falls back for invalid values', () => {
    const before = Date.now();
    const value = coerceTimestamp('not-a-date');
    expect(value).toBeGreaterThanOrEqual(before);
  });
});

describe('resolvePrivateWireMessage', () => {
  it('maps own ciphertext placeholders to a text-like display type', () => {
    const resolved = resolvePrivateWireMessage({
      msgType: 'encrypted',
      content: '{"type":"prekey","ct":"abc"}',
      isOwn: true,
    });
    expect(resolved.type).toBe('text');
    expect(resolved.decryptionStatus).toBe('ciphertext');
    expect(resolved.content).toBe('🔒 [本地加密消息]');
    expect(resolved.isEncrypted).toBe(true);
  });

  it('maps failed inbound history to a safe text placeholder', () => {
    const resolved = resolvePrivateWireMessage({
      msgType: 'encrypted',
      content: '{"type":"prekey","ct":"abc"}',
      isOwn: false,
      decryptResult: { success: false, error: 'no session' },
      failedPlaceholder: HISTORY_DECRYPT_PLACEHOLDER,
    });
    expect(resolved.type).toBe('text');
    expect(resolved.decryptionFailed).toBe(true);
    expect(resolved.decryptionStatus).toBe('failed');
    expect(resolved.content).toBe(HISTORY_DECRYPT_PLACEHOLDER);
  });

  it('keeps decrypted inner type when it is a real bubble type', () => {
    const resolved = resolvePrivateWireMessage({
      msgType: 'encrypted',
      content: '{"type":"prekey"}',
      isOwn: false,
      decryptResult: {
        success: true,
        plaintext: JSON.stringify({ content: '今晚吃饭吗', msgType: 'text' }),
      },
    });
    expect(resolved.type).toBe('text');
    expect(resolved.content).toBe('今晚吃饭吗');
    expect(resolved.decryptionStatus).toBe('decrypted');
  });

  it('does not keep inner msgType=encrypted after a successful decrypt', () => {
    const resolved = resolvePrivateWireMessage({
      msgType: 'encrypted',
      content: '{"type":"prekey"}',
      isOwn: false,
      decryptResult: {
        success: true,
        plaintext: JSON.stringify({ content: 'hi', msgType: 'encrypted' }),
      },
    });
    expect(resolved.type).toBe('text');
    expect(resolved.content).toBe('hi');
  });
});

describe('parseDecryptedInnerPayload', () => {
  it('accepts a raw string payload', () => {
    expect(parseDecryptedInnerPayload('"hello"')).toEqual({ content: 'hello', msgType: 'text', extra: {} });
  });
});

describe('sanitizeMessages', () => {
  it('coerces leftover encrypted types and bad timestamps so the list can render', () => {
    const [message] = sanitizeMessages([
      {
        id: 'm1',
        chatId: 'c1',
        senderId: 'u1',
        content: { type: 'prekey' },
        type: 'encrypted',
        timestamp: '2024-03-09T12:00:00.000Z',
        isEncrypted: true,
        reactions: null,
        status: 'sent',
        decryptionStatus: 'failed',
      },
    ]);
    expect(message.type).toBe('text');
    expect(typeof message.content).toBe('string');
    expect(message.timestamp).toBe(Date.parse('2024-03-09T12:00:00.000Z'));
    expect(message.reactions).toEqual({});
  });

  it('drops items without id/senderId', () => {
    expect(sanitizeMessages([null, { id: '', senderId: 'u1' }, { id: 'm1' }])).toEqual([]);
  });
});

describe('looksLikeCiphertext', () => {
  it('does not throw when ChatBubble passes a non-string leftover payload', () => {
    expect(looksLikeCiphertext(undefined)).toBe(false);
    expect(looksLikeCiphertext({ type: 'prekey' } as any)).toBe(false);
  });
});

describe('failedDecryptResults', () => {
  it('maps a thrown batch to per-message failures', () => {
    const results = failedDecryptResults([{ id: 'a' }, { id: 'b' }], new Error('worker down'));
    expect(results).toEqual([
      { id: 'a', success: false, error: 'worker down' },
      { id: 'b', success: false, error: 'worker down' },
    ]);
  });
});
