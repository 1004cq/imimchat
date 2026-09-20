import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { applyAuthMeUser, CURRENT_USER } from './store.ts';

describe('applyAuthMeUser', () => {
  const store: Record<string, string> = {};

  beforeEach(() => {
    for (const key of Object.keys(store)) delete store[key];
    CURRENT_USER.id = 'me';
    CURRENT_USER.name = '用户';
    CURRENT_USER.uniqueId = 'user';
    CURRENT_USER.avatar = '';
    CURRENT_USER.bio = '';
    vi.stubGlobal('localStorage', {
      getItem: (key: string) => store[key] ?? null,
      setItem: (key: string, value: string) => { store[key] = value; },
      removeItem: (key: string) => { delete store[key]; },
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('reads go-api { user } shape and refreshes user_avatar', () => {
    const user = applyAuthMeUser({
      user: {
        id: '1',
        username: 'qing',
        nickname: 'Qing',
        avatar: '/api/media/0670b9a72a7f207e7fb977f2',
        bio: 'hi',
      },
    });
    expect(user).toEqual({
      id: '1',
      username: 'qing',
      nickname: 'Qing',
      avatar: '/api/media/0670b9a72a7f207e7fb977f2',
      bio: 'hi',
    });
    expect(store.user_avatar).toBe('/api/media/0670b9a72a7f207e7fb977f2');
    expect(CURRENT_USER.avatar).toBe('/api/media/0670b9a72a7f207e7fb977f2');
  });

  it('accepts a flat Node-style payload', () => {
    const user = applyAuthMeUser({
      id: '1',
      username: 'qing',
      nickname: 'Qing',
      avatar: '/api/media/abc',
    });
    expect(user?.avatar).toBe('/api/media/abc');
    expect(store.user_avatar).toBe('/api/media/abc');
  });

  it('writes empty avatar so initials can replace a stale local URL', () => {
    store.user_avatar = '/stale.png';
    CURRENT_USER.avatar = '/stale.png';
    const user = applyAuthMeUser({
      user: { id: '0', username: 'u0', nickname: 'User0', avatar: null },
    });
    expect(user?.avatar).toBe('');
    expect(store.user_avatar).toBe('');
  });

  it('returns null when the payload has no user id', () => {
    expect(applyAuthMeUser({})).toBeNull();
    expect(applyAuthMeUser(null)).toBeNull();
  });
});
