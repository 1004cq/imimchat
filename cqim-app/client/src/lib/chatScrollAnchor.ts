/**
 * 按 chatId 持久化会话阅读锚点（messageId），避免虚拟列表动态高度下 scrollTop 错位。
 */

const STORAGE_PREFIX = 'cqim:chat-scroll-anchor:';

export function saveChatScrollAnchor(chatId: string, messageId: string): void {
  if (!chatId || !messageId) return;
  try {
    sessionStorage.setItem(STORAGE_PREFIX + chatId, messageId);
  } catch {
    /* ignore quota / private mode */
  }
}

export function loadChatScrollAnchor(chatId: string): string | null {
  if (!chatId) return null;
  try {
    return sessionStorage.getItem(STORAGE_PREFIX + chatId);
  } catch {
    return null;
  }
}

export function clearChatScrollAnchor(chatId: string): void {
  if (!chatId) return;
  try {
    sessionStorage.removeItem(STORAGE_PREFIX + chatId);
  } catch {
    /* ignore */
  }
}
