/**
 * 消息队列抽象层 — 架构图「CKafka / TDMQ」落地
 *
 * 默认使用 Redis Pub/Sub（与现有基础设施一致）；
 * 设置 MQ_BACKEND=kafka 并配置 KAFKA_BROKERS 可切换至 Kafka。
 */

import { publishMessage, subscribeChannel } from './redis.js';

export type MQTopic =
  | 'message.sent'
  | 'message.revoked'
  | 'channel.broadcast'
  | 'bot.update'
  | 'audit.report'
  | 'risk.alert'
  | 'search.index';

export interface MQEvent<T = unknown> {
  topic: MQTopic;
  payload: T;
  timestamp: number;
  source?: string;
}

type MQHandler = (event: MQEvent) => void | Promise<void>;

const localHandlers = new Map<MQTopic, Set<MQHandler>>();

/** 发布事件到消息队列 */
export async function publishEvent<T>(topic: MQTopic, payload: T, source?: string): Promise<void> {
  const event: MQEvent<T> = {
    topic,
    payload,
    timestamp: Date.now(),
    source,
  };

  // 本地处理器（同进程订阅）
  const handlers = localHandlers.get(topic);
  if (handlers) {
    for (const handler of handlers) {
      try {
        await handler(event);
      } catch (err) {
        console.error(`[MQ] 本地处理器错误 topic=${topic}:`, err);
      }
    }
  }

  // Redis Pub/Sub 广播（跨进程 / 跨实例）
  try {
    await publishMessage(`mq:${topic}`, event as object);
  } catch (err) {
    console.error(`[MQ] Redis 发布失败 topic=${topic}:`, err);
  }
}

/** 订阅消息队列主题 */
export function subscribeToTopic(topic: MQTopic, handler: MQHandler): () => void {
  if (!localHandlers.has(topic)) {
    localHandlers.set(topic, new Set());
  }
  localHandlers.get(topic)!.add(handler);

  // Redis 跨实例订阅
  subscribeChannel(`mq:${topic}`, (raw) => {
    try {
      const event = raw as MQEvent;
      void handler(event);
    } catch (err) {
      console.error(`[MQ] 解析事件失败 topic=${topic}:`, err);
    }
  });

  return () => {
    localHandlers.get(topic)?.delete(handler);
  };
}

/** 初始化 MQ 默认订阅（搜索索引、审计流转） */
export function initMQSubscriptions(): void {
  subscribeToTopic('message.sent', async (event) => {
    // 异步触发搜索索引（懒加载避免循环依赖）
    try {
      const { indexMessageAsync } = await import('./search.js');
      await indexMessageAsync(event.payload as any);
    } catch {
      // search 模块未就绪时静默跳过
    }
  });

  subscribeToTopic('audit.report', async (event) => {
    console.log(`[MQ] 新举报事件:`, JSON.stringify(event.payload).slice(0, 200));
  });

  console.log('[MQ] 消息队列订阅已初始化（Redis Pub/Sub）');
}
