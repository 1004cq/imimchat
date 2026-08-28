import type { Message } from '@/lib/store';

/** 保证消息列表始终是可安全 map 的数组 */
export function sanitizeMessages(list: unknown): Message[] {
  if (!Array.isArray(list)) return [];
  return list
    .filter((item): item is Message => {
      if (!item || typeof item !== 'object') return false;
      const msg = item as Message;
      return typeof msg.id === 'string' && msg.id.length > 0 && typeof msg.senderId === 'string';
    })
    .map(msg => ({
      ...msg,
      reactions: msg.reactions && typeof msg.reactions === 'object' ? msg.reactions : {},
      content: typeof msg.content === 'string' ? msg.content : (msg.content == null ? '' : String(msg.content)),
      timestamp: typeof msg.timestamp === 'number' ? msg.timestamp : Date.now(),
    }));
}
