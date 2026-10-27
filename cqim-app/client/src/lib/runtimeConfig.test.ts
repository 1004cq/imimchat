import { afterEach, describe, expect, it, vi } from 'vitest';
import { ensureRuntime, resolveSignalBase, type RuntimeConfig } from './runtimeConfig';

describe('runtimeConfig', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('marks multi when apiCount or gatewayCount > 1', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({
        ok: true,
        json: async () => ({
          mode: 'multi',
          nodeId: 'api-1',
          signalPath: '/signal',
          groupWsPath: '/ws/group',
          signalUrls: ['wss://n1.example/signal', 'wss://n2.example/signal'],
          apiCount: 2,
          gatewayCount: 2,
        }),
      })),
    );
    const cfg = await ensureRuntime(true);
    expect(cfg.mode).toBe('multi');
    expect(cfg.signalUrls).toHaveLength(2);
  });

  it('sticky-picks signal URL in multi mode', () => {
    const runtime: RuntimeConfig = {
      mode: 'multi',
      nodeId: 'api-1',
      signalPath: '/signal',
      groupWsPath: '/ws/group',
      signalUrls: ['wss://n1.example/signal', 'wss://n2.example/signal'],
      apiCount: 2,
      gatewayCount: 2,
      fetchedAt: Date.now(),
    };
    const a = resolveSignalBase(runtime, 'user-a');
    const b = resolveSignalBase(runtime, 'user-a');
    expect(a).toBe(b);
    expect(runtime.signalUrls).toContain(a);
  });

  it('uses same-origin path in single mode', () => {
    const runtime: RuntimeConfig = {
      mode: 'single',
      nodeId: 'api-1',
      signalPath: '/signal',
      groupWsPath: '/ws/group',
      signalUrls: [],
      apiCount: 1,
      gatewayCount: 0,
      fetchedAt: Date.now(),
    };
    const base = resolveSignalBase(runtime, 'user-a');
    expect(base).toContain('/signal');
  });
});
