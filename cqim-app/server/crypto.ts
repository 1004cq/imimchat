/**
 * server/crypto.ts - E2EE 加密基础设施
 *
 * 提供端到端加密（E2EE）所需的服务端支持：
 * 1. ECDH 密钥交换 API（公钥注册与分发）
 * 2. AES-256-GCM 服务端加密存储工具
 * 3. HMAC-SHA256 消息完整性校验
 * 4. 密钥轮换与过期管理
 *
 * 安全设计原则：
 * - 服务端仅存储公钥，私钥永远不离开客户端
 * - 消息在客户端加密后传输，服务端无法解密消息内容
 * - HMAC 签名确保消息在传输过程中未被篡改
 */
import { Router, Request, Response } from 'express';
import crypto from 'crypto';
import prisma from './db.js';
import { resolveSigningPublicKey } from './prekey-bundle.js';
import { userAuth } from './auth.js';
import { consumePreKey, mergePreKeys, type PreKeyPool } from './prekey-pool.js';
import type { Prisma } from '@prisma/client';

const router = Router();
router.use(userAuth);
router.use((req, res, next) => {
  res.setHeader('Cache-Control', 'no-store');
  if (req.method === 'POST' && req.body?.userId && req.body.userId !== (req as any).user.id) {
    return res.status(403).json({ error: '不能修改其他用户的密钥' });
  }
  next();
});

// All pool readers/writers use the SAME database lock, including across Node
// processes. Keep the legacy available-array format for existing consumers.
async function withPreKeyLock<T>(userId: string, work: (tx: Prisma.TransactionClient) => Promise<T>): Promise<T> {
  return prisma.$transaction(async tx => {
    // executeRaw avoids asking Prisma to deserialize PostgreSQL's void type.
    await tx.$executeRaw`SELECT pg_advisory_xact_lock(hashtext(${`e2ee:${userId}`}))`;
    return work(tx);
  });
}

async function readPool(tx: Prisma.TransactionClient, userId: string): Promise<PreKeyPool> {
  const available = await tx.systemConfig.findUnique({ where: { key: `e2ee:prekeys:${userId}` } });
  const consumed = await tx.systemConfig.findUnique({ where: { key: `e2ee:consumed-prekeys:${userId}` } });
  return { available: available ? JSON.parse(available.value) : [], consumed: consumed ? JSON.parse(consumed.value) : [] };
}

async function writePool(tx: Prisma.TransactionClient, userId: string, pool: PreKeyPool): Promise<void> {
  for (const [key, value] of [[`e2ee:prekeys:${userId}`, pool.available], [`e2ee:consumed-prekeys:${userId}`, pool.consumed]] as const) {
    await tx.systemConfig.upsert({ where: { key }, update: { value: JSON.stringify(value) }, create: { key, value: JSON.stringify(value) } });
  }
}

// ============================================================
// 1. AES-256-GCM 加解密工具（服务端存储加密）
// ============================================================

const ENCRYPTION_ALGORITHM = 'aes-256-gcm';
const IV_LENGTH = 12;  // GCM 推荐 12 字节 IV
const TAG_LENGTH = 16;  // 认证标签长度

/**
 * 获取服务端加密密钥
 * 从环境变量读取，如果不存在则自动生成并持久化到数据库
 */
let _serverEncryptionKey: Buffer | null = null;

async function getServerEncryptionKey(): Promise<Buffer> {
  if (_serverEncryptionKey) return _serverEncryptionKey;

  // 优先从环境变量读取
  if (process.env.ENCRYPTION_KEY) {
    _serverEncryptionKey = Buffer.from(process.env.ENCRYPTION_KEY, 'hex');
    return _serverEncryptionKey;
  }

  // 从数据库读取或生成
  try {
    const config = await prisma.systemConfig.findUnique({ where: { key: 'encryptionKey' } });
    if (config?.value) {
      _serverEncryptionKey = Buffer.from(JSON.parse(config.value), 'hex');
    } else {
      const newKey = crypto.randomBytes(32);
      await prisma.systemConfig.create({
        data: { key: 'encryptionKey', value: JSON.stringify(newKey.toString('hex')) },
      });
      _serverEncryptionKey = newKey;
      console.log('[Crypto] 已生成新的服务端加密密钥');
    }
  } catch {
    // 回退：使用固定派生密钥（仅开发环境）
    _serverEncryptionKey = crypto.scryptSync('cqim-dev-key', 'cqim-salt', 32);
  }

  return _serverEncryptionKey;
}

/**
 * AES-256-GCM 加密
 * 返回格式：base64(IV + ciphertext + authTag)
 */
export async function encryptData(plaintext: string): Promise<string> {
  const key = await getServerEncryptionKey();
  const iv = crypto.randomBytes(IV_LENGTH);
  const cipher = crypto.createCipheriv(ENCRYPTION_ALGORITHM, key, iv);

  let encrypted = cipher.update(plaintext, 'utf8');
  encrypted = Buffer.concat([encrypted, cipher.final()]);
  const authTag = cipher.getAuthTag();

  // IV(12) + ciphertext(N) + authTag(16)
  const result = Buffer.concat([iv, encrypted, authTag]);
  return result.toString('base64');
}

/**
 * AES-256-GCM 解密
 * 输入格式：base64(IV + ciphertext + authTag)
 */
export async function decryptData(encryptedBase64: string): Promise<string> {
  const key = await getServerEncryptionKey();
  const data = Buffer.from(encryptedBase64, 'base64');

  const iv = data.subarray(0, IV_LENGTH);
  const authTag = data.subarray(data.length - TAG_LENGTH);
  const ciphertext = data.subarray(IV_LENGTH, data.length - TAG_LENGTH);

  const decipher = crypto.createDecipheriv(ENCRYPTION_ALGORITHM, key, iv);
  decipher.setAuthTag(authTag);

  let decrypted = decipher.update(ciphertext);
  decrypted = Buffer.concat([decrypted, decipher.final()]);
  return decrypted.toString('utf8');
}

// ============================================================
// 2. HMAC-SHA256 消息完整性校验
// ============================================================

/**
 * 生成 HMAC-SHA256 签名
 * 用于验证消息在传输过程中未被篡改
 */
export function generateHMAC(message: string, secret: string): string {
  return crypto.createHmac('sha256', secret).update(message).digest('hex');
}

/**
 * 验证 HMAC-SHA256 签名
 * 使用 timingSafeEqual 防止时序攻击
 */
export function verifyHMAC(message: string, signature: string, secret: string): boolean {
  const expected = generateHMAC(message, secret);
  try {
    return crypto.timingSafeEqual(
      Buffer.from(signature, 'hex'),
      Buffer.from(expected, 'hex')
    );
  } catch {
    return false;
  }
}

// ============================================================
// 3. ECDH 密钥交换 API
// ============================================================

/**
 * POST /api/crypto/register-key
 * 客户端注册自己的 ECDH 公钥
 * body: { userId, publicKey, deviceId }
 *
 * 客户端生成 ECDH 密钥对后，将公钥注册到服务端。
 * 其他用户可以通过 /get-key 获取该公钥，完成密钥交换。
 */
router.post('/register-key', async (req: Request, res: Response) => {
  const { userId, publicKey, deviceId } = req.body;

  if (!userId || !publicKey) {
    return res.status(400).json({ error: '缺少必要参数' });
  }

  // 验证公钥格式（Base64 编码的 ECDH 公钥）
  if (!/^[A-Za-z0-9+/=]+$/.test(publicKey) || publicKey.length < 40) {
    return res.status(400).json({ error: '公钥格式无效' });
  }

  try {
    // Upsert：同一用户同一设备只保留最新公钥
    await prisma.systemConfig.upsert({
      where: { key: `e2ee:pubkey:${userId}:${deviceId || 'default'}` },
      update: {
        value: JSON.stringify({
          publicKey,
          updatedAt: new Date().toISOString(),
        }),
      },
      create: {
        key: `e2ee:pubkey:${userId}:${deviceId || 'default'}`,
        value: JSON.stringify({
          publicKey,
          createdAt: new Date().toISOString(),
          updatedAt: new Date().toISOString(),
        }),
      },
    });

    res.json({ success: true });
  } catch (err: any) {
    console.error('[Crypto] 注册公钥失败:', err.message);
    res.status(500).json({ error: '注册公钥失败' });
  }
});

/**
 * GET /api/crypto/get-key?userId=xxx&deviceId=xxx
 * 获取指定用户的 ECDH 公钥
 */
router.get('/get-key', async (req: Request, res: Response) => {
  const { userId, deviceId = 'default' } = req.query as { userId: string; deviceId?: string };

  if (!userId) {
    return res.status(400).json({ error: '缺少 userId' });
  }

  try {
    const config = await prisma.systemConfig.findUnique({
      where: { key: `e2ee:pubkey:${userId}:${deviceId}` },
    });

    if (!config?.value) {
      return res.status(404).json({ error: '未找到公钥' });
    }

    const data = JSON.parse(config.value);
    res.json({ publicKey: data.publicKey, updatedAt: data.updatedAt });
  } catch (err: any) {
    console.error('[Crypto] 获取公钥失败:', err.message);
    res.status(500).json({ error: '获取公钥失败' });
  }
});

/**
 * POST /api/crypto/verify-message
 * 验证消息的 HMAC 签名完整性
 * body: { message, signature, senderId }
 */
router.post('/verify-message', async (req: Request, res: Response) => {
  const { message, signature, senderId } = req.body;

  if (!message || !signature || !senderId) {
    return res.status(400).json({ error: '缺少必要参数' });
  }

  try {
    // 使用发送者的公钥作为 HMAC 密钥的一部分
    const config = await prisma.systemConfig.findUnique({
      where: { key: `e2ee:pubkey:${senderId}:default` },
    });

    if (!config?.value) {
      return res.status(404).json({ error: '发送者公钥未注册' });
    }

    const { publicKey } = JSON.parse(config.value);
    const hmacKey = crypto.createHash('sha256').update(publicKey).digest('hex');
    const valid = verifyHMAC(message, signature, hmacKey);

    res.json({ valid });
  } catch (err: any) {
    console.error('[Crypto] 验证签名失败:', err.message);
    res.status(500).json({ error: '验证失败' });
  }
});

// ============================================================
// 4. Signal Protocol PreKey Bundle API
// ============================================================

/**
 * POST /api/crypto/register-bundle
 * 客户端上传 Signal Protocol PreKey Bundle
 * body: {
 *   userId: string,
 *   registrationId: number,
 *   identityKey: string,       // Base64 编码的 ECDH 身份公钥
 *   signingPublicKey?: string, // Base64 编码的 ECDSA 签名公钥（Signed PreKey 验签）
 *   signedPreKey: {
 *     keyId: number,
 *     publicKey: string,       // Base64
 *     signature: string,       // Base64
 *   },
 *   preKeys: Array<{ keyId: number, publicKey: string }>,  // One-Time PreKeys
 * }
 */
router.post('/register-bundle', async (req: Request, res: Response) => {
  const { userId, registrationId, identityKey, signingPublicKey, signedPreKey, preKeys } = req.body;

  if (!userId || !identityKey || !signedPreKey) {
    return res.status(400).json({ error: '缺少必要参数' });
  }

  try {
    await withPreKeyLock(userId, async tx => {
    const existingBundle = await tx.systemConfig.findUnique({
      where: { key: `e2ee:bundle:${userId}` },
    });
    const storedSigningPublicKey = resolveSigningPublicKey(
      signingPublicKey,
      identityKey,
      existingBundle?.value,
    );

    // 存储 ECDH 身份公钥、ECDSA 签名公钥和签名预密钥
    await tx.systemConfig.upsert({
      where: { key: `e2ee:bundle:${userId}` },
      update: {
        value: JSON.stringify({
          registrationId,
          identityKey,
          signingPublicKey: storedSigningPublicKey,
          signedPreKey,
          updatedAt: new Date().toISOString(),
        }),
      },
      create: {
        key: `e2ee:bundle:${userId}`,
        value: JSON.stringify({
          registrationId,
          identityKey,
          signingPublicKey: storedSigningPublicKey,
          signedPreKey,
          createdAt: new Date().toISOString(),
          updatedAt: new Date().toISOString(),
        }),
      },
    });

    const sameIdentity = existingBundle && JSON.parse(existingBundle.value).identityKey === identityKey;
    const pool = sameIdentity ? await readPool(tx, userId) : { available: [], consumed: [] };
    // Validate ID conflicts before pruning keys absent from the uploaded pool.
    if (Array.isArray(preKeys)) mergePreKeys(pool, preKeys);
    // A full registration advertises exactly the private keys still retained
    // by this installation. Preserve tombstones, not stale available keys.
    await writePool(tx, userId, mergePreKeys({ available: Array.isArray(preKeys) ? [] : pool.available, consumed: pool.consumed }, Array.isArray(preKeys) ? preKeys : []));
    });

    console.log(`[Crypto] 用户 ${userId} 注册 PreKey Bundle 成功, preKeys: ${preKeys?.length || 0}`);
    res.json({ success: true });
  } catch (err: any) {
    console.error('[Crypto] 注册 Bundle 失败:', err.message);
    res.status(500).json({ error: '注册 Bundle 失败' });
  }
});

/**
 * GET /api/crypto/get-bundle?userId=xxx
 * 获取指定用户的 PreKey Bundle（包含一个 One-Time PreKey）
 * 返回后自动消费该 One-Time PreKey
 */
router.get('/get-bundle', async (req: Request, res: Response) => {
  const { userId } = req.query as { userId: string };

  if (!userId) {
    return res.status(400).json({ error: '缺少 userId' });
  }

  try {
    const result = await withPreKeyLock(userId, async tx => {
    const bundleConfig = await tx.systemConfig.findUnique({
      where: { key: `e2ee:bundle:${userId}` },
    });

    if (!bundleConfig?.value) {
      return null;
    }

    const bundle = JSON.parse(bundleConfig.value);

    const consumed = consumePreKey(await readPool(tx, userId));
    if (consumed.key) await writePool(tx, userId, consumed.pool);
    return {
      registrationId: bundle.registrationId,
      identityKey: bundle.identityKey,
      signingPublicKey: bundle.signingPublicKey || null,
      signedPreKey: bundle.signedPreKey,
      preKey: consumed.key || null,
    };
    });
    if (!result) return res.status(404).json({ error: '用户未注册 E2EE Bundle' });
    res.json(result);
  } catch (err: any) {
    console.error('[Crypto] 获取 Bundle 失败:', err.message);
    res.status(500).json({ error: '获取 Bundle 失败' });
  }
});

/**
 * GET /api/crypto/prekey-count?userId=xxx
 * 查询用户剩余的 One-Time PreKey 数量
 * 客户端可以定期检查，当数量低于阈值时补充新的 PreKeys
 */
router.get('/prekey-count', async (req: Request, res: Response) => {
  const { userId } = req.query as { userId: string };
  if (userId !== (req as any).user.id) return res.status(403).json({ error: '只能查询自己的密钥池' });

  if (!userId) {
    return res.status(400).json({ error: '缺少 userId' });
  }

  try {
    const preKeysConfig = await prisma.systemConfig.findUnique({
      where: { key: `e2ee:prekeys:${userId}` },
    });

    let count = 0;
    if (preKeysConfig?.value) {
      const preKeys = JSON.parse(preKeysConfig.value);
      count = Array.isArray(preKeys) ? preKeys.length : 0;
    }

    res.json({ count });
  } catch (err: any) {
    console.error('[Crypto] 查询 PreKey 数量失败:', err.message);
    res.status(500).json({ error: '查询失败' });
  }
});

/**
 * POST /api/crypto/replenish-prekeys
 * 补充 One-Time PreKeys
 * body: { userId: string, preKeys: Array<{ keyId: number, publicKey: string }> }
 */
router.post('/replenish-prekeys', async (req: Request, res: Response) => {
  const { userId, preKeys } = req.body;

  if (!userId || !Array.isArray(preKeys) || preKeys.length === 0) {
    return res.status(400).json({ error: '缺少必要参数' });
  }

  try {
    const count = await withPreKeyLock(userId, async tx => {
      const bundle = await tx.systemConfig.findUnique({ where: { key: `e2ee:bundle:${userId}` } });
      if (!bundle) throw new Error('Register identity before replenishing');
      const pool = mergePreKeys(await readPool(tx, userId), preKeys);
      await writePool(tx, userId, pool);
      return pool.available.length;
    });
    res.json({ success: true, totalCount: count });
  } catch (err: any) {
    console.error('[Crypto] 补充 PreKeys 失败:', err.message);
    res.status(500).json({ error: '补充失败' });
  }
});

// ============================================================
// 5. 工具函数导出
// ============================================================

/** 生成安全随机字节（Base64 编码） */
export function generateSecureRandom(bytes: number = 32): string {
  return crypto.randomBytes(bytes).toString('base64');
}

/** 安全比较两个字符串（防时序攻击） */
export function secureCompare(a: string, b: string): boolean {
  if (a.length !== b.length) return false;
  try {
    return crypto.timingSafeEqual(Buffer.from(a), Buffer.from(b));
  } catch {
    return false;
  }
}

export default router;
