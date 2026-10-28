/* IMIM Web Push service worker. May show ephemeral preview body from the server. */
self.addEventListener('push', (event) => {
  let data = {};
  try { data = event.data ? event.data.json() : {}; } catch (_) { data = {}; }
  if (data.encrypted !== true || data.type !== 'encrypted_message') return;

  const chatId = typeof data.chatId === 'string' ? data.chatId : '';
  const title = typeof data.title === 'string' && data.title.trim() ? data.title.trim() : 'IMIM';
  const body = typeof data.body === 'string' && data.body.trim() ? data.body.trim() : '发来一条消息';
  const options = {
    body,
    icon: '/icon-192.png',
    badge: '/icon-128.png',
    tag: chatId ? `cqim-chat-${chatId}` : 'cqim-encrypted-message',
    renotify: true,
    data: { chatId, messageId: data.messageId || '', senderId: data.senderId || '' },
  };
  event.waitUntil(self.registration.showNotification(title, options));
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const data = event.notification.data || {};
  const chatId = typeof data.chatId === 'string' ? data.chatId : '';
  event.waitUntil((async () => {
    const clients = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
    for (const client of clients) {
      if ('focus' in client) {
        await client.focus();
        client.postMessage({ type: 'cqim:open-chat-from-notification', chatId });
        return;
      }
    }
    if (self.clients.openWindow) {
      await self.clients.openWindow(chatId ? `/?chatId=${encodeURIComponent(chatId)}` : '/');
    }
  })());
});
