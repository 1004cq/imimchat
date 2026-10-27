import { describe, expect, it } from 'vitest';
import { mergePrivateMessages } from '../messageMerge.ts';
import type { Message } from '../store.ts';

function message(partial: Partial<Message> & Pick<Message, 'id' | 'content'>): Message {
  return {
    chatId: 'chat-1',
    senderId: 'peer',
    type: 'text',
    timestamp: 1,
    isEncrypted: true,
    reactions: {},
    status: 'sent',
    ...partial,
  };
}

describe('mergePrivateMessages', () => {
  it('keeps decrypted history when a refresh supplies ciphertext or a failed decrypt', () => {
    const existing = [message({
      id: 'm1',
      content: 'hello',
      decryptionStatus: 'decrypted',
      timestamp: 10,
    })];
    const ciphertext = [message({
      id: 'm1',
      content: '🔒 加密消息',
      decryptionStatus: 'ciphertext',
      timestamp: 10,
    })];
    const failed = [message({
      id: 'm1',
      content: '🔒 旧安全会话消息（无法恢复）',
      decryptionStatus: 'failed',
      decryptionFailed: true,
      timestamp: 10,
    })];

    expect(mergePrivateMessages(existing, ciphertext)[0].content).toBe('hello');
    expect(mergePrivateMessages(existing, failed)[0].content).toBe('hello');
  });

  it('replaces a ciphertext placeholder once decryption succeeds', () => {
    const existing = [message({
      id: 'm1',
      content: '🔒 加密消息',
      decryptionStatus: 'ciphertext',
    })];
    const decrypted = [message({
      id: 'm1',
      content: 'hello',
      decryptionStatus: 'decrypted',
    })];
    expect(mergePrivateMessages(existing, decrypted)[0].content).toBe('hello');
  });

  it('keeps an older decrypted copy beside a newly loaded message', () => {
    const existing = [message({ id: 'm1', content: 'kept', decryptionStatus: 'decrypted', timestamp: 1 })];
    const incoming = [message({ id: 'm2', content: '🔒 加密消息', decryptionStatus: 'ciphertext', timestamp: 2 })];
    expect(mergePrivateMessages(existing, incoming).map(item => item.id)).toEqual(['m1', 'm2']);
  });
});
