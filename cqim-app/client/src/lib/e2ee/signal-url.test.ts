import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { authenticatedSignalUrl } from './signal-url';

function installBrowserStubs() {
  const store = new Map<string, string>();
  vi.stubGlobal('localStorage', {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => {
      store.set(k, v);
    },
    removeItem: (k: string) => {
      store.delete(k);
    },
    clear: () => store.clear(),
  });
  vi.stubGlobal('window', {
    location: { origin: 'https://im.example', protocol: 'https:', host: 'im.example' },
  });
}

describe('authenticatedSignalUrl', () => {
  beforeEach(() => {
    installBrowserStubs();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('returns null without a login token', () => {
    expect(authenticatedSignalUrl('alice')).toBeNull();
  });

  it('builds a same-origin /signal URL with token when no runtime cache', () => {
    localStorage.setItem('user_token', 'tok');
    const url = new URL(authenticatedSignalUrl('alice')!);
    expect(url.protocol).toMatch(/^ws/);
    expect(url.pathname).toBe('/signal');
    expect(url.searchParams.get('token')).toBe('tok');
    expect(url.searchParams.get('userId')).toBe('alice');
  });
});
