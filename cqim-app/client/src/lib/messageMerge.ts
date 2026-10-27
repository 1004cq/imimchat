import type { Message } from './store';

/** 本地已经解出的明文。刷新后不能被密文占位或失败占位盖掉。 */
export function isUsableDecryptedMessage(message: Partial<Message> | null | undefined): boolean {
  if (!message || message.isRecalled || message.decryptionFailed) return false;
  if (message.decryptionStatus === 'failed' || message.decryptionStatus === 'ciphertext') return false;
  if (typeof message.content !== 'string' || message.content.length === 0) return false;
  if (message.decryptionStatus === 'decrypted') return true;
  return message.isEncrypted === false;
}

export function isCiphertextPlaceholder(message: Partial<Message> | null | undefined): boolean {
  if (!message || message.isRecalled) return false;
  return message.decryptionStatus === 'ciphertext'
    || message.decryptionStatus === 'failed'
    || message.decryptionFailed === true;
}

/**
 * 按 id 合并私聊消息。
 * 已解密展示稿优先于服务端密文和再次解密失败的占位，避免刷新把历史记录刷成空。
 */
export function mergePrivateMessages(existing: Message[], incoming: Message[]): Message[] {
  const byId = new Map<string, Message>();
  for (const message of existing) {
    if (message?.id) byId.set(message.id, message);
  }

  for (const message of incoming) {
    if (!message?.id) continue;
    const previous = byId.get(message.id);
    if (!previous) {
      byId.set(message.id, message);
      continue;
    }

    if (message.isRecalled) {
      byId.set(message.id, {
        ...previous,
        ...message,
        isRecalled: true,
        content: message.content || '消息已撤回',
      });
      continue;
    }

    const previousUsable = isUsableDecryptedMessage(previous);
    const incomingUsable = isUsableDecryptedMessage(message);
    if (previousUsable && (isCiphertextPlaceholder(message) || incomingUsable)) {
      byId.set(message.id, {
        ...message,
        ...previous,
        status: message.status || previous.status,
        timestamp: previous.timestamp || message.timestamp,
      });
      continue;
    }

    if (!previousUsable && incomingUsable) {
      byId.set(message.id, { ...previous, ...message });
      continue;
    }

    byId.set(message.id, { ...previous, ...message });
  }

  return Array.from(byId.values()).sort((a, b) => {
    const timeDiff = Number(a.timestamp || 0) - Number(b.timestamp || 0);
    return timeDiff || a.id.localeCompare(b.id);
  });
}
