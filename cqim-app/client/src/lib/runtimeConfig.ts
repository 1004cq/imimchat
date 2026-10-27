/**
 * 从 /api/runtime 拉取单机/多机拓扑，供信令连接选路。
 * 多机且配置了 PUBLIC_SIGNAL_URLS 时按 userId 粘性选节点；
 * 否则始终走同源 /signal（由 Nginx LB 分发）。
 */

export type DeployMode = 'single' | 'multi';

export interface RuntimeConfig {
  mode: DeployMode;
  nodeId: string;
  signalPath: string;
  groupWsPath: string;
  signalUrls: string[];
  apiCount: number;
  gatewayCount: number;
  fetchedAt: number;
}

const CACHE_TTL_MS = 30_000;
let cache: RuntimeConfig | null = null;
let inflight: Promise<RuntimeConfig> | null = null;

function fallbackRuntime(): RuntimeConfig {
  return {
    mode: 'single',
    nodeId: 'local',
    signalPath: '/signal',
    groupWsPath: '/ws/group',
    signalUrls: [],
    apiCount: 1,
    gatewayCount: 0,
    fetchedAt: Date.now(),
  };
}

function stickyIndex(key: string, modulo: number): number {
  if (modulo <= 0) return 0;
  let hash = 0;
  for (let i = 0; i < key.length; i += 1) {
    hash = (hash * 31 + key.charCodeAt(i)) >>> 0;
  }
  return hash % modulo;
}

async function fetchRuntime(): Promise<RuntimeConfig> {
  try {
    const res = await fetch('/api/runtime', { credentials: 'same-origin', cache: 'no-store' });
    if (!res.ok) return fallbackRuntime();
    const data = (await res.json()) as Partial<RuntimeConfig> & { ok?: boolean };
    return {
      mode: data.mode === 'multi' ? 'multi' : 'single',
      nodeId: typeof data.nodeId === 'string' ? data.nodeId : 'local',
      signalPath: typeof data.signalPath === 'string' ? data.signalPath : '/signal',
      groupWsPath: typeof data.groupWsPath === 'string' ? data.groupWsPath : '/ws/group',
      signalUrls: Array.isArray(data.signalUrls)
        ? data.signalUrls.filter((u): u is string => typeof u === 'string' && u.length > 0)
        : [],
      apiCount: typeof data.apiCount === 'number' ? data.apiCount : 1,
      gatewayCount: typeof data.gatewayCount === 'number' ? data.gatewayCount : 0,
      fetchedAt: Date.now(),
    };
  } catch {
    return fallbackRuntime();
  }
}

/** 获取（并短缓存）运行时拓扑 */
export async function ensureRuntime(force = false): Promise<RuntimeConfig> {
  if (!force && cache && Date.now() - cache.fetchedAt < CACHE_TTL_MS) {
    return cache;
  }
  if (!force && inflight) return inflight;
  inflight = fetchRuntime().then((cfg) => {
    cache = cfg;
    inflight = null;
    return cfg;
  });
  return inflight;
}

export function getCachedRuntime(): RuntimeConfig | null {
  return cache;
}

/**
 * Resolve WebSocket base for /signal.
 * - single / multi behind LB: same origin + signalPath
 * - multi with absolute signalUrls: sticky pick by userId (or random key)
 */
export function resolveSignalBase(runtime: RuntimeConfig, stickyKey = ''): string {
  if (runtime.mode === 'multi' && runtime.signalUrls.length > 0) {
    const picked = runtime.signalUrls[stickyIndex(stickyKey || 'anon', runtime.signalUrls.length)];
    return picked.replace(/\/$/, '');
  }
  if (typeof window === 'undefined') {
    return runtime.signalPath || '/signal';
  }
  const path = runtime.signalPath || '/signal';
  const url = new URL(path, window.location.origin);
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
  // Return origin+pathname without query (caller adds token)
  return `${url.protocol}//${url.host}${url.pathname}`;
}
