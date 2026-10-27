import { describe, expect, it } from 'vitest';
import { parseCsvUrls, resolveDeployMode } from './topology.ts';

describe('topology helpers', () => {
  it('parses PUBLIC_SIGNAL_URLS', () => {
    expect(parseCsvUrls('wss://a/signal, wss://b/signal/')).toEqual([
      'wss://a/signal',
      'wss://b/signal',
    ]);
    expect(parseCsvUrls('')).toEqual([]);
  });

  it('auto-detects single vs multi from alive counts', () => {
    expect(resolveDeployMode(1, 1, 'auto')).toBe('single');
    expect(resolveDeployMode(1, 0, 'auto')).toBe('single');
    expect(resolveDeployMode(2, 1, 'auto')).toBe('multi');
    expect(resolveDeployMode(1, 2, 'auto')).toBe('multi');
  });

  it('honors forced DEPLOY_MODE', () => {
    expect(resolveDeployMode(1, 1, 'multi')).toBe('multi');
    expect(resolveDeployMode(5, 5, 'single')).toBe('single');
    expect(resolveDeployMode(2, 2, 'AUTO')).toBe('multi');
    expect(resolveDeployMode(1, 1, undefined)).toBe('single');
  });
});
