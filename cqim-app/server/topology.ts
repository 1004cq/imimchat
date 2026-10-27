/**
 * 单机 / 多机拓扑探测（基于 Redis 存活键）
 *
 * - Gateway 写入 gw:alive:{nodeId}
 * - 本 API 进程写入 api:alive:{nodeId}
 * - GET /api/runtime 供客户端自动选路连接 /signal
 */
import os from 'os';
import { redis } from './redis.js';

const API_ALIVE_PREFIX = 'api:alive:';
const GW_ALIVE_PREFIX = 'gw:alive:';
const ALIVE_TTL_SECONDS = 30;

export type DeployMode = 'single' | 'multi';

export interface AliveNode {
  nodeId: string;
  kind: 'api' | 'gateway';
}

export interface RuntimeTopology {
  mode: DeployMode;
  nodeId: string;
  /** Same-origin relative path preferred when behind Nginx LB */
  signalPath: string;
  groupWsPath: string;
  /**
   * Absolute WebSocket base URLs for /signal when multi-node without a shared LB.
   * Empty → client uses window.location.origin + signalPath.
   */
  signalUrls: string[];
  apis: AliveNode[];
  gateways: AliveNode[];
  apiCount: number;
  gatewayCount: number;
}

function resolveNodeId(): string {
  return (
    process.env.API_NODE_ID ||
    process.env.GATEWAY_NODE_ID ||
    process.env.GATEWAY_ID ||
    os.hostname() ||
    'api-unknown'
  );
}

export function getLocalApiNodeId(): string {
  return resolveNodeId();
}

function parseCsvUrls(raw: string | undefined): string[] {
  if (!raw) return [];
  return raw
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
    .map((u) => u.replace(/\/$/, ''));
}

async function scanAlive(prefix: string, kind: 'api' | 'gateway'): Promise<AliveNode[]> {
  const nodes: AliveNode[] = [];
  let cursor = '0';
  do {
    const [next, batch] = await redis.scan(cursor, 'MATCH', `${prefix}*`, 'COUNT', 100);
    cursor = next;
    for (const key of batch) {
      const nodeId = key.slice(prefix.length);
      if (nodeId) nodes.push({ nodeId, kind });
    }
  } while (cursor !== '0');
  nodes.sort((a, b) => a.nodeId.localeCompare(b.nodeId));
  return nodes;
}

/** API 进程心跳：写入 api:alive:{nodeId} */
export async function refreshApiAlive(): Promise<void> {
  const nodeId = resolveNodeId();
  try {
    await redis.setex(`${API_ALIVE_PREFIX}${nodeId}`, ALIVE_TTL_SECONDS, String(Date.now()));
  } catch (error) {
    console.error('[topology] 刷新 api:alive 失败:', error);
  }
}

export async function clearApiAlive(): Promise<void> {
  const nodeId = resolveNodeId();
  try {
    await redis.del(`${API_ALIVE_PREFIX}${nodeId}`);
  } catch {
    /* ignore */
  }
}

let heartbeatTimer: ReturnType<typeof setInterval> | null = null;

export function startApiTopologyHeartbeat(): void {
  void refreshApiAlive();
  if (heartbeatTimer) return;
  heartbeatTimer = setInterval(() => {
    void refreshApiAlive();
  }, 15_000);
  if (typeof heartbeatTimer.unref === 'function') heartbeatTimer.unref();
}

export function stopApiTopologyHeartbeat(): void {
  if (heartbeatTimer) {
    clearInterval(heartbeatTimer);
    heartbeatTimer = null;
  }
  void clearApiAlive();
}

export async function getRuntimeTopology(): Promise<RuntimeTopology> {
  let apis: AliveNode[] = [];
  let gateways: AliveNode[] = [];
  try {
    [apis, gateways] = await Promise.all([
      scanAlive(API_ALIVE_PREFIX, 'api'),
      scanAlive(GW_ALIVE_PREFIX, 'gateway'),
    ]);
  } catch (error) {
    console.error('[topology] 扫描存活节点失败，按单机降级:', error);
  }

  const localId = resolveNodeId();
  if (!apis.some((n) => n.nodeId === localId)) {
    apis = [{ nodeId: localId, kind: 'api' }, ...apis];
  }

  const apiCount = apis.length;
  const gatewayCount = gateways.length;
  const mode: DeployMode = apiCount > 1 || gatewayCount > 1 ? 'multi' : 'single';

  return {
    mode,
    nodeId: localId,
    signalPath: '/signal',
    groupWsPath: '/ws/group',
    signalUrls: parseCsvUrls(process.env.PUBLIC_SIGNAL_URLS),
    apis,
    gateways,
    apiCount,
    gatewayCount,
  };
}
