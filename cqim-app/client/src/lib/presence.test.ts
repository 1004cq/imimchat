import { beforeEach, describe, expect, it, vi } from 'vitest';
import { PRESENCE_HEARTBEAT_MS, reportPresence } from './presence.ts';

describe('reportPresence', () => {
  const fetchMock = vi.fn().mockResolvedValue({ ok: true });

  beforeEach(() => {
    fetchMock.mockClear();
    vi.stubGlobal('fetch', fetchMock);
    vi.stubGlobal('localStorage', {
      getItem: (key: string) => (key === 'user_token' ? 'test-token' : null),
    });
  });

  it('heartbeats faster than Redis TTL (90s)', () => {
    expect(PRESENCE_HEARTBEAT_MS).toBeLessThan(90_000);
  });

  it('POSTs background so a hidden tab is not treated as skip-APNs', async () => {
    await reportPresence('background', null, { keepalive: true });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/presence');
    expect(init.method).toBe('POST');
    expect(init.keepalive).toBe(true);
    expect(init.headers.Authorization).toBe('Bearer test-token');
    expect(JSON.parse(init.body)).toEqual({ state: 'background', activeChatId: null });
  });
});
