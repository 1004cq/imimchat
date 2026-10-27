import { describe, expect, it } from 'vitest';

// Pure helpers mirrored for unit coverage without Redis.
function parseCsvUrls(raw: string | undefined): string[] {
  if (!raw) return [];
  return raw
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
    .map((u) => u.replace(/\/$/, ''));
}

describe('topology helpers', () => {
  it('parses PUBLIC_SIGNAL_URLS', () => {
    expect(parseCsvUrls('wss://a/signal, wss://b/signal/')).toEqual([
      'wss://a/signal',
      'wss://b/signal',
    ]);
    expect(parseCsvUrls('')).toEqual([]);
  });
});
