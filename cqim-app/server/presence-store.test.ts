import { afterEach, describe, expect, it, vi } from 'vitest';

const store = new Map<string, string>();

vi.mock('./redis.js', () => ({
  redis: {
    get: async (key: string) => store.get(key) ?? null,
    set: async (key: string, value: string) => {
      store.set(key, value);
      return 'OK';
    },
    del: async (...keys: string[]) => {
      for (const key of keys) store.delete(key);
      return keys.length;
    },
  },
}));

import { getActiveChatId, getPresence, setPresence, shouldSkipApns } from './presence.ts';

describe('setPresence Redis helpers', () => {
  afterEach(() => {
    store.clear();
  });

  it('writes foreground + activeChat and skips only that DM', async () => {
    await setPresence('user-1', 'foreground', 'chat-1');
    expect(await getPresence('user-1')).toBe('foreground');
    expect(await getActiveChatId('user-1')).toBe('chat-1');
    expect(await shouldSkipApns('user-1', 'chat-1')).toBe(true);
    expect(await shouldSkipApns('user-1', 'chat-2')).toBe(false);
  });

  it('clears activeChat when Web reports null', async () => {
    await setPresence('user-1', 'foreground', 'chat-1');
    await setPresence('user-1', 'foreground', null);
    expect(await getPresence('user-1')).toBe('foreground');
    expect(await getActiveChatId('user-1')).toBeNull();
    expect(await shouldSkipApns('user-1', 'chat-1')).toBe(true);
  });

  it('deletes keys on offline so APNs is not skipped', async () => {
    await setPresence('user-1', 'foreground', 'chat-1');
    await setPresence('user-1', 'offline');
    expect(store.has('user:presence:user-1')).toBe(false);
    expect(await getPresence('user-1')).toBe('offline');
    expect(await shouldSkipApns('user-1', 'chat-1')).toBe(false);
  });

  it('never skips APNs for background even if activeChat matches', async () => {
    await setPresence('user-1', 'background', 'chat-1');
    expect(await shouldSkipApns('user-1', 'chat-1')).toBe(false);
  });
});
