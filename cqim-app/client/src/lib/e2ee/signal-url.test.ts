import { afterEach, describe, expect, it, vi } from 'vitest';
import { authenticatedSignalUrl } from './signal-url';
afterEach(() => vi.unstubAllGlobals());
describe('signal connection credentials', () => {
  it('requires login and does not fall back to a claimed account', () => {
    vi.stubGlobal('localStorage', { getItem: () => null });
    expect(authenticatedSignalUrl('alice')).toBeNull();
  });
  it('uses the authenticated token and encodes special characters safely', () => {
    vi.stubGlobal('localStorage', { getItem: () => 'synthetic-token+/?=' });
    vi.stubGlobal('window', { location: { origin: 'https://example.test' } });
    const url = new URL(authenticatedSignalUrl('alice')!);
    expect(url.protocol).toBe('wss:');
    expect(url.pathname).toBe('/signal');
    expect(url.searchParams.get('userId')).toBe('alice');
    expect(url.searchParams.get('token')).toBe('synthetic-token+/?=');
  });
});
