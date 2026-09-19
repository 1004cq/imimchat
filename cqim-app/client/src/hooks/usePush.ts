import { useCallback, useEffect, useRef } from 'react';
import { Capacitor } from '@capacitor/core';

let PushNotifications: any = null;

async function getPushNotifications() {
  if (!Capacitor.isNativePlatform() || Capacitor.getPlatform() !== 'ios') return null;
  if (PushNotifications) return PushNotifications;
  try {
    PushNotifications = (await import('@capacitor/push-notifications')).PushNotifications;
    return PushNotifications;
  } catch {
    return null;
  }
}

function urlBase64ToUint8Array(value: string): Uint8Array {
  const padding = '='.repeat((4 - (value.length % 4)) % 4);
  const raw = atob((value + padding).replace(/-/g, '+').replace(/_/g, '/'));
  return Uint8Array.from([...raw].map((char) => char.charCodeAt(0)));
}

async function registerApnsToken(token: string) {
  const auth = localStorage.getItem('user_token');
  if (!auth) return;
  await fetch('/api/apns/token', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${auth}` },
    body: JSON.stringify({ token, platform: 'ios', environment: import.meta.env.PROD ? 'production' : 'sandbox', kind: 'alert' }),
  });
}

async function registerWebPush(subscription: PushSubscription) {
  const auth = localStorage.getItem('user_token');
  if (!auth) return;
  const response = await fetch('/api/web-push/subscription', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${auth}` },
    body: JSON.stringify({ subscription: subscription.toJSON() }),
  });
  if (!response.ok) throw new Error(`Web Push subscription failed: ${response.status}`);
}

export function usePush(isLoggedIn: boolean) {
  const cleanupRef = useRef<(() => void) | null>(null);

  const initWebPush = useCallback(async () => {
    if (Capacitor.isNativePlatform() || !('serviceWorker' in navigator) || !('PushManager' in window) || !('Notification' in window)) return;
    try {
      const keyResponse = await fetch('/api/web-push/public-key');
      if (!keyResponse.ok) return;
      const config = await keyResponse.json();
      if (!config.enabled || !config.publicKey) return;
      const permission = Notification.permission === 'default' ? await Notification.requestPermission() : Notification.permission;
      if (permission !== 'granted') return;
      const registration = await navigator.serviceWorker.register('/sw.js', { scope: '/' });
      const subscription = await registration.pushManager.getSubscription() || await registration.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: urlBase64ToUint8Array(config.publicKey),
      });
      await registerWebPush(subscription);
      const onMessage = (event: MessageEvent) => {
        if (event.data?.type === 'cqim:open-chat-from-notification') {
          window.dispatchEvent(new CustomEvent('cqim:open-chat-from-notification', { detail: { chatId: event.data.chatId } }));
        }
      };
      navigator.serviceWorker.addEventListener('message', onMessage);
      cleanupRef.current = () => navigator.serviceWorker.removeEventListener('message', onMessage);
    } catch (error) {
      console.warn('[WebPush] initialization skipped', error);
    }
  }, []);

  const initApns = useCallback(async () => {
    const push = await getPushNotifications();
    if (!push) return;
    try {
      let permission = await push.checkPermissions();
      if (permission.receive === 'prompt') permission = await push.requestPermissions();
      if (permission.receive !== 'granted') return;
      await push.addListener('registration', ({ value }: { value: string }) => void registerApnsToken(value));
      await push.addListener('pushNotificationReceived', (notification: any) => {
        if (notification.data?.chatId) window.dispatchEvent(new CustomEvent('cqim:open-chat-from-notification', { detail: { chatId: notification.data.chatId } }));
      });
      await push.addListener('pushNotificationActionPerformed', (action: any) => {
        if (action.notification?.data?.chatId) window.dispatchEvent(new CustomEvent('cqim:open-chat-from-notification', { detail: { chatId: action.notification.data.chatId } }));
      });
      await push.register();
    } catch (error) {
      console.warn('[APNs] initialization skipped', error);
    }
  }, []);

  useEffect(() => {
    if (!isLoggedIn) return;
    void initApns();
    void initWebPush();
    return () => {
      cleanupRef.current?.();
      cleanupRef.current = null;
      getPushNotifications().then((push) => push?.removeAllListeners?.().catch(() => {}));
    };
  }, [isLoggedIn, initApns, initWebPush]);
}
