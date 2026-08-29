import type { Message } from '@/lib/store';
import { coerceMessageContent } from '@/lib/privateMessageMapper';

const VALID_TYPES = new Set<Message['type']>([
  'text', 'image', 'video', 'file', 'voice', 'system', 'call', 'location', 'location_share', 'sticker',
]);

/** 保证消息列表始终是可安全 map 的数组 */
export function sanitizeMessages(list: unknown): Message[] {
  if (!Array.isArray(list)) return [];
  return list
    .filter((item): item is Message => {
      if (!item || typeof item !== 'object') return false;
      const msg = item as Message;
      return typeof msg.id === 'string' && msg.id.length > 0 && typeof msg.senderId === 'string';
    })
    .map(msg => {
      const rawType = msg.type as string | undefined;
      const type = rawType && VALID_TYPES.has(rawType as Message['type'])
        ? (rawType as Message['type'])
        : 'text';
      return {
        ...msg,
        type,
        reactions: msg.reactions && typeof msg.reactions === 'object' && !Array.isArray(msg.reactions)
          ? msg.reactions
          : {},
        content: coerceMessageContent(msg.content),
        decryptedContent: msg.decryptedContent != null ? coerceMessageContent(msg.decryptedContent) : undefined,
        timestamp: typeof msg.timestamp === 'number' && Number.isFinite(msg.timestamp) ? msg.timestamp : Date.now(),
      };
    });
}
