/**
 * server/apns.ts - 自建 APNs 推送服务
 *
 * 功能：
 * 1. POST /api/apns/token       - 注册/更新用户的 APNs Device Token
 * 2. POST /api/apns/voip-token  - 注册/更新用户的 VoIP Token
 * 3. DELETE /api/apns/token      - 清除用户的 APNs Token
 * 4. sendAPNsPush()             - 发送普通离线消息推送
 * 5. sendVoIPPush()             - 发送音视频来电 VoIP 推送
 *
 * 使用 p8 证书（Token Authentication）直连 Apple APNs HTTP/2 服务器
 * 同一个 p8 证书同时支持普通推送和 VoIP 推送（topic 不同）
 *
 * 支持多个 p8 证书自动尝试（fallback 机制）
 */
import { Router, Request, Response } from 'express';
import prisma from './db.js';
import { userAuth } from './auth.js';
import http2 from 'http2';
import crypto from 'crypto';

const router = Router();

// ============ 配置 ============
const TEAM_ID = process.env.APPLE_TEAM_ID || '';
const BUNDLE_ID = process.env.APPLE_BUNDLE_ID || 'com.imim.chat';
const IS_PRODUCTION = process.env.NODE_ENV === 'production';
const APNS_HOST = IS_PRODUCTION ? 'api.push.apple.com' : 'api.sandbox.push.apple.com';

interface P8Key {
  keyId: string;
  keyData: string;
}

// P8 私钥只从环境变量读取，禁止写入数据库或镜像。
const p8Keys: P8Key[] = process.env.APNS_KEY_ID && (process.env.APNS_P8_KEY || process.env.APNS_PRIVATE_KEY)
  ? [{ keyId: process.env.APNS_KEY_ID, keyData: (process.env.APNS_P8_KEY || process.env.APNS_PRIVATE_KEY || '').replace(/\\n/g, '\n') }]
  : [];

// ============ JWT Token 生成 ============

// JWT Token 缓存（每个 keyId 一个，有效期 50 分钟，Apple 限制 60 分钟）
const jwtCache = new Map<string, { token: string; expiry: number }>();

function generateJWT(keyId: string, keyData: string): string {
  const cached = jwtCache.get(keyId);
  if (cached && Date.now() < cached.expiry) {
    return cached.token;
  }

  const header = Buffer.from(JSON.stringify({
    alg: 'ES256',
    kid: keyId,
  })).toString('base64url');

  const now = Math.floor(Date.now() / 1000);
  const claims = Buffer.from(JSON.stringify({
    iss: TEAM_ID,
    iat: now,
  })).toString('base64url');

  const signingInput = `${header}.${claims}`;

  const sign = crypto.createSign('SHA256');
  sign.update(signingInput);
  const signature = sign.sign(keyData);

  // 将 DER 格式签名转换为 raw (r || s) 格式
  const rawSig = derToRaw(signature);
  const sig = rawSig.toString('base64url');

  const jwt = `${signingInput}.${sig}`;

  // 缓存 50 分钟
  jwtCache.set(keyId, { token: jwt, expiry: Date.now() + 50 * 60 * 1000 });

  return jwt;
}

/**
 * 将 DER 编码的 ECDSA 签名转换为 raw (r || s) 格式
 */
function derToRaw(derSig: Buffer): Buffer {
  // DER: 0x30 [total-len] 0x02 [r-len] [r] 0x02 [s-len] [s]
  let offset = 2; // skip 0x30 and total length
  
  // Read r
  if (derSig[offset] !== 0x02) throw new Error('Invalid DER signature');
  offset++;
  const rLen = derSig[offset];
  offset++;
  let r = derSig.subarray(offset, offset + rLen);
  offset += rLen;
  
  // Read s
  if (derSig[offset] !== 0x02) throw new Error('Invalid DER signature');
  offset++;
  const sLen = derSig[offset];
  offset++;
  let s = derSig.subarray(offset, offset + sLen);
  
  // Remove leading zeros and pad to 32 bytes
  if (r.length > 32) r = r.subarray(r.length - 32);
  if (s.length > 32) s = s.subarray(s.length - 32);
  
  const raw = Buffer.alloc(64);
  r.copy(raw, 32 - r.length);
  s.copy(raw, 64 - s.length);
  
  return raw;
}

// ============ HTTP/2 APNs 发送 ============

interface APNsResult {
  success: boolean;
  statusCode?: number;
  reason?: string;
  keyId?: string;
}

/**
 * 通过 HTTP/2 发送 APNs 推送
 */
async function sendToAPNs(
  deviceToken: string,
  payload: object,
  topic: string,
  pushType: 'alert' | 'voip' = 'alert',
  priority: number = 10
): Promise<APNsResult> {
  if (p8Keys.length === 0) {
    console.error('[APNs] 没有可用的 p8 证书');
    return { success: false, reason: 'no_p8_keys' };
  }

  // 依次尝试每个 p8 证书
  for (const key of p8Keys) {
    try {
      const result = await sendWithKey(key, deviceToken, payload, topic, pushType, priority);
      if (result.success) {
        return result;
      }
      // 如果是 Token 无效（403 InvalidProviderToken），尝试下一个 key
      if (result.statusCode === 403 && result.reason === 'InvalidProviderToken') {
        console.warn(`[APNs] KeyID=${key.keyId} 无效，尝试下一个...`);
        // 清除该 key 的 JWT 缓存
        jwtCache.delete(key.keyId);
        continue;
      }
      // 其他错误直接返回
      return result;
    } catch (err) {
      console.error(`[APNs] KeyID=${key.keyId} 发送异常:`, err);
      continue;
    }
  }

  return { success: false, reason: 'all_keys_failed' };
}

function sendWithKey(
  key: P8Key,
  deviceToken: string,
  payload: object,
  topic: string,
  pushType: 'alert' | 'voip',
  priority: number
): Promise<APNsResult> {
  return new Promise((resolve) => {
    const jwt = generateJWT(key.keyId, key.keyData);
    const payloadStr = JSON.stringify(payload);

    const client = http2.connect(`https://${APNS_HOST}`);

    client.on('error', (err) => {
      console.error(`[APNs] HTTP/2 连接错误 (KeyID=${key.keyId}):`, err);
      client.close();
      resolve({ success: false, reason: 'connection_error', keyId: key.keyId });
    });

    const headers: http2.OutgoingHttpHeaders = {
      ':method': 'POST',
      ':path': `/3/device/${deviceToken}`,
      'authorization': `bearer ${jwt}`,
      'apns-topic': topic,
      'apns-push-type': pushType,
      'apns-priority': String(priority),
      'apns-expiration': '0',
    };

    const req = client.request(headers);

    let responseData = '';
    let statusCode = 0;

    req.on('response', (headers) => {
      statusCode = headers[':status'] as number;
    });

    req.on('data', (chunk) => {
      responseData += chunk;
    });

    req.on('end', () => {
      client.close();

      if (statusCode === 200) {
        console.log(`[APNs] 推送成功 (KeyID=${key.keyId})`);
        resolve({ success: true, statusCode, keyId: key.keyId });
      } else {
        let reason = 'unknown';
        try {
          const parsed = JSON.parse(responseData);
          reason = parsed.reason || 'unknown';
        } catch {}
        console.warn(`[APNs] 推送失败 (KeyID=${key.keyId}): status=${statusCode} reason=${reason}`);
        resolve({ success: false, statusCode, reason, keyId: key.keyId });
      }
    });

    req.on('error', (err) => {
      client.close();
      console.error(`[APNs] 请求错误 (KeyID=${key.keyId}):`, err);
      resolve({ success: false, reason: 'request_error', keyId: key.keyId });
    });

    // 设置超时
    req.setTimeout(10000, () => {
      req.close();
      client.close();
      resolve({ success: false, reason: 'timeout', keyId: key.keyId });
    });

    req.write(payloadStr);
    req.end();
  });
}

// ============ API 路由 ============
router.use(userAuth);

/**
 * POST /api/apns/token
 * 注册或更新用户的 APNs Device Token
 * Body: { token: string }
 */
router.post('/token', async (req: Request, res: Response) => {
  try {
    const currentUser = (req as any).user;
    const { token } = req.body;

    if (!token || typeof token !== 'string') {
      return res.status(400).json({ error: '缺少 token' });
    }

    const platform = req.body.platform === 'ios' ? 'ios' : String(req.body.platform || 'ios');
    const environment = req.body.environment === 'production' ? 'production' : 'sandbox';
    const kind = req.body.kind === 'voip' ? 'voip' : 'alert';
    await prisma.pushDeviceToken.upsert({
      where: { token_kind: { token, kind } },
      create: { userId: currentUser.id, token, platform, environment, kind },
      update: { userId: currentUser.id, platform, environment },
    });
    console.log(`[APNs] 用户 ${currentUser.id} 注册 Device Token`);
    return res.json({ success: true });
  } catch (err) {
    console.error('[APNs] Token 注册失败:', err);
    return res.status(500).json({ error: '服务器内部错误' });
  }
});

/**
 * POST /api/apns/voip-token
 * 注册或更新用户的 VoIP Token
 * Body: { voipToken: string }
 */
router.post('/voip-token', async (req: Request, res: Response) => {
  try {
    const currentUser = (req as any).user;
    const { voipToken } = req.body;

    if (!voipToken || typeof voipToken !== 'string') {
      return res.status(400).json({ error: '缺少 voipToken' });
    }

    await prisma.pushDeviceToken.upsert({
      where: { token_kind: { token: voipToken, kind: 'voip' } },
      create: { userId: currentUser.id, token: voipToken, platform: 'ios', environment: IS_PRODUCTION ? 'production' : 'sandbox', kind: 'voip' },
      update: { userId: currentUser.id, platform: 'ios', environment: IS_PRODUCTION ? 'production' : 'sandbox' },
    });
    console.log(`[APNs] 用户 ${currentUser.id} 注册 VoIP Token`);
    return res.json({ success: true });
  } catch (err) {
    console.error('[APNs] VoIP Token 注册失败:', err);
    return res.status(500).json({ error: '服务器内部错误' });
  }
});

/**
 * DELETE /api/apns/token
 * 用户登出时清除 Token
 */
router.delete('/token', async (req: Request, res: Response) => {
  try {
    const currentUser = (req as any).user;
    await prisma.pushDeviceToken.deleteMany({ where: { userId: currentUser.id, platform: 'ios' } });
    console.log(`[APNs] 用户 ${currentUser.id} 清除 Token`);
    return res.json({ success: true });
  } catch (err) {
    console.error('[APNs] Token 清除失败:', err);
    return res.status(500).json({ error: '服务器内部错误' });
  }
});

export default router;

// ============ 推送发送工具函数 ============
export interface APNsPushPayload {
  toUserId: string;
  title: string;
  body: string;
  customData?: Record<string, unknown>;
}

export async function sendAPNsPush(payload: APNsPushPayload): Promise<boolean> {
  try {
    const devices = await prisma.pushDeviceToken.findMany({
      where: { userId: payload.toUserId, platform: 'ios', kind: 'alert' },
    });
    if (!devices.length) return false;
    let delivered = false;
    for (const device of devices) {
      const result = await sendToAPNs(device.token, {
        aps: { alert: { title: payload.title, body: payload.body }, 'mutable-content': 1, sound: 'default', badge: 1 },
        ...(payload.customData || {}),
      }, BUNDLE_ID, 'alert', 10);
      if (result.success) {
        delivered = true;
      } else if (result.reason === 'BadDeviceToken' || result.reason === 'Unregistered') {
        await prisma.pushDeviceToken.delete({ where: { id: device.id } }).catch(() => {});
      }
    }
    return delivered;
  } catch (error) {
    console.error('[APNs] 推送请求失败:', error);
    return false;
  }
}

export interface VoIPPushPayload {
  /** 接收方用户 ID */
  toUserId: string;
  /** 来电者名称 */
  callerName: string;
  /** 来电 ID */
  callId: string;
  /** 来电者 ID */
  callerId: string;
  /** 来电者头像 URL */
  callerAvatar?: string;
  /** 通话类型 */
  callType?: 'audio' | 'video';
  /** 房间 ID */
  roomId?: string;
}

/**
 * 向指定用户发送 VoIP 来电推送
 * 如果用户没有注册 VoIP Token，则静默跳过
 */
export async function sendVoIPPush(payload: VoIPPushPayload): Promise<boolean> {
  try {
    const user = await prisma.user.findUnique({
      where: { id: payload.toUserId },
      select: { pushTokens: { where: { platform: 'ios', kind: 'voip' }, take: 1 } },
    });

    const device = user?.pushTokens[0];
    if (!device) {
      return false;
    }

    const voipPayload = {
      type: 'call_invite',
      call_id: payload.callId,
      caller_id: payload.callerId,
      caller_name: payload.callerName,
      caller_avatar: payload.callerAvatar || '',
      call_type: payload.callType || 'audio',
      room_id: payload.roomId || '',
    };

    // VoIP 推送 topic 必须添加 .voip 后缀
    const result = await sendToAPNs(
      device.token,
      voipPayload,
      `${BUNDLE_ID}.voip`,
      'voip',
      10
    );

    if (result.success) {
      console.log(`[APNs] VoIP 推送成功: userId=${payload.toUserId} keyId=${result.keyId}`);
      return true;
    } else {
      console.error(`[APNs] VoIP 推送失败: userId=${payload.toUserId} reason=${result.reason}`);
      if (result.reason === 'BadDeviceToken' || result.reason === 'Unregistered') {
        await prisma.pushDeviceToken.delete({ where: { id: device.id } }).catch(() => {});
      }
      return false;
    }
  } catch (err) {
    console.error('[APNs] VoIP 推送请求失败:', err);
    return false;
  }
}
