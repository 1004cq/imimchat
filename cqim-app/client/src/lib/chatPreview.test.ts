import { formatChatListPreview, looksLikeCiphertext, preferLocalChatPreview, sanitizePreviewText } from './chatPreview';
import { messageMediaPatch } from './mediaFields';

function assert(condition: unknown, message: string) {
  if (!condition) throw new Error(message);
}

assert(looksLikeCiphertext('{"_mls":true,"ct":"abc"}'), 'mls envelope should look like ciphertext');
assert(looksLikeCiphertext('{"ratchetKey":"x","ciphertext":"y"}'), 'signal envelope should look like ciphertext');
assert(!looksLikeCiphertext('今晚吃饭吗'), 'plain text should not look like ciphertext');

assert(sanitizePreviewText('{"_mls":true,"ct":"abc"}') === '🔒 [加密消息]', 'sanitize envelope');
assert(formatChatListPreview({ type: 'image', content: 'cipher' }) === '[图片]', 'image preview');
assert(formatChatListPreview({ decryptionStatus: 'failed', content: 'x' }) === '🔒 无法解密', 'failed preview');
assert(formatChatListPreview({ isRecalled: true, content: 'hi' }) === '消息已撤回', 'recall preview');

assert(
  preferLocalChatPreview('🔒 [加密消息]', '今晚吃饭吗') === '今晚吃饭吗',
  'keep local decrypted preview over server lock placeholder'
);
assert(
  preferLocalChatPreview('新的明文', '旧的明文') === '新的明文',
  'incoming plaintext wins when it is not opaque'
);

const media = messageMediaPatch({
  imageUrl: '/api/media/files/x.jpg',
  fileKey: 'abc',
  iv: 'def',
  isEncryptedMedia: true,
});
assert(media.imageUrl === '/api/media/files/x.jpg', 'imageUrl copied from extra');
assert(media.fileKey === 'abc' && media.iv === 'def', 'file key/iv copied from extra');

console.log('chatPreview tests passed');
