/**
 * 跨节点 IM 推送：通过 Redis Pub/Sub 将 WebSocket 载荷投递到持有对端连接的那台 Node/Gateway。
 *
 * 频道：cqim:im:push
 * 消息格式：{ userId: string, payload: object | string }
 */
import { redisPub, redisSub } from './redis.js';

export const IM_PUSH_CHANNEL = 'cqim:im:push';

export interface ImPushEnvelope {
  userId: string;
  payload: unknown;
}

type ImPushHandler = (userId: string, payload: unknown) => void;

let subscriberBound = false;
const handlers = new Set<ImPushHandler>();

function ensureSubscriber() {
  if (subscriberBound) return;
  subscriberBound = true;

  redisSub.subscribe(IM_PUSH_CHANNEL, (err) => {
    if (err) {
      console.error('[IM Push] 订阅失败:', err.message);
    } else {
      console.log(`[IM Push] 已订阅 ${IM_PUSH_CHANNEL}`);
    }
  });

  redisSub.on('message', (channel, data) => {
    if (channel !== IM_PUSH_CHANNEL) return;
    try {
      const envelope = JSON.parse(data) as ImPushEnvelope;
      if (!envelope?.userId) return;
      for (const handler of handlers) {
        try {
          handler(envelope.userId, envelope.payload);
        } catch (handlerErr) {
          console.error('[IM Push] handler 异常:', handlerErr);
        }
      }
    } catch (parseErr) {
      console.error('[IM Push] 消息解析失败:', parseErr);
    }
  });
}

/** 发布跨节点 IM 推送；失败仅打日志，不抛错 */
export async function publishImPush(userId: string, payload: unknown): Promise<void> {
  if (!userId) return;
  try {
    const envelope: ImPushEnvelope = { userId, payload };
    await redisPub.publish(IM_PUSH_CHANNEL, JSON.stringify(envelope));
  } catch (err) {
    console.error(`[IM Push] PUBLISH 失败 userId=${userId}:`, err);
  }
}

/** 订阅跨节点 IM 推送（每台 Node 进程启动时注册一次） */
export function subscribeImPush(handler: ImPushHandler): () => void {
  ensureSubscriber();
  handlers.add(handler);
  return () => {
    handlers.delete(handler);
  };
}
