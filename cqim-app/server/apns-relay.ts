/**
 * Cross-node APNs relay: HK often cannot reach api.push.apple.com; YN can.
 * When a local send fails, publish the same alert payload for peer nodes to retry once.
 */
import { redisPub, redisSub } from './redis.js';

export const APNS_RELAY_CHANNEL = 'cqim:apns:relay';

export interface ApnsRelayEnvelope {
  toUserId: string;
  title: string;
  body: string;
  customData?: Record<string, unknown>;
  fromNodeId: string;
  /** Prevent infinite re-relay */
  relayed: true;
}

type RelayHandler = (envelope: ApnsRelayEnvelope) => void;

let subscriberBound = false;
const handlers = new Set<RelayHandler>();

function nodeId(): string {
  return process.env.API_NODE_ID
    || process.env.GATEWAY_NODE_ID
    || process.env.GATEWAY_ID
    || 'api-unknown';
}

function ensureSubscriber() {
  if (subscriberBound) return;
  subscriberBound = true;

  redisSub.subscribe(APNS_RELAY_CHANNEL, (err) => {
    if (err) {
      console.error('[APNs Relay] 订阅失败:', err.message);
    } else {
      console.log(`[APNs Relay] 已订阅 ${APNS_RELAY_CHANNEL}`);
    }
  });

  redisSub.on('message', (channel, data) => {
    if (channel !== APNS_RELAY_CHANNEL) return;
    try {
      const envelope = JSON.parse(data) as ApnsRelayEnvelope;
      if (!envelope?.toUserId || envelope.relayed !== true) return;
      if (envelope.fromNodeId === nodeId()) return;
      for (const handler of handlers) {
        try {
          handler(envelope);
        } catch (handlerErr) {
          console.error('[APNs Relay] handler 异常:', handlerErr);
        }
      }
    } catch (parseErr) {
      console.error('[APNs Relay] 消息解析失败:', parseErr);
    }
  });
}

export async function publishApnsRelay(payload: {
  toUserId: string;
  title: string;
  body: string;
  customData?: Record<string, unknown>;
}): Promise<void> {
  if (!payload.toUserId) return;
  if (process.env.APNS_RELAY === '0') return;
  try {
    const envelope: ApnsRelayEnvelope = {
      ...payload,
      fromNodeId: nodeId(),
      relayed: true,
    };
    await redisPub.publish(APNS_RELAY_CHANNEL, JSON.stringify(envelope));
  } catch (err) {
    console.error('[APNs Relay] PUBLISH 失败:', err);
  }
}

export function subscribeApnsRelay(handler: RelayHandler): () => void {
  ensureSubscriber();
  handlers.add(handler);
  return () => {
    handlers.delete(handler);
  };
}
