import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';
import express from 'express';
import type { Server } from 'node:http';
const db = vi.hoisted(() => {
  const rows = new Map<string, any>();
  let tail = Promise.resolve();
  const tx = {
    $executeRaw: async () => 1,
    systemConfig: {
      findUnique: async ({ where }: any) => structuredClone(rows.get(where.key) || null),
      upsert: async ({ where, update, create }: any) => {
        rows.set(where.key, structuredClone(rows.has(where.key) ? { ...rows.get(where.key), ...update } : create));
      },
    },
  };
  return { ...tx, $transaction: (work: any) => {
    // Model the PostgreSQL advisory lock. Live DB locking is a separate check.
    const result = tail.then(() => work(tx));
    tail = result.then(() => undefined, () => undefined);
    return result;
  } };
});
vi.mock('./db.js', () => ({ default: db }));
vi.mock('./auth.js', () => ({ userAuth: (req: any, res: any, next: any) => {
  const id = req.headers.authorization?.replace('Bearer ', '');
  if (!id) return res.status(401).json({ error: 'login required' });
  req.user = { id }; next();
} }));
import router from './crypto';
import { mergePreKeys, consumePreKey } from './prekey-pool';
let server: Server, url: string;
beforeAll(async () => {
  const app = express(); app.use(express.json()); app.use(router);
  await new Promise<void>(resolve => { server = app.listen(0, '127.0.0.1', resolve); });
  url = `http://127.0.0.1:${(server.address() as any).port}`;
});
afterAll(() => new Promise<void>(resolve => server.close(() => resolve())));
const post = (path: string, body: any, owner = 'alice') => fetch(url + path, {
  method: 'POST', headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${owner}` }, body: JSON.stringify(body),
});
const registration = (identityKey = 'identity', preKeys = [{ keyId: 1, publicKey: 'key-1' }, { keyId: 2, publicKey: 'key-2' }]) => ({
  userId: 'alice', registrationId: 1, identityKey, signedPreKey: { keyId: 1, publicKey: 'signed', signature: 'signature' }, preKeys,
});

describe('key directory lifecycle', () => {
  it('rejects anonymous reads and cross-account writes', async () => {
    expect((await fetch(url + '/get-bundle?userId=alice')).status).toBe(401);
    for (const route of ['/register-bundle', '/register-key', '/replenish-prekeys']) {
      expect((await post(route, registration(), 'mallory')).status).toBe(403);
    }
  });
  it('atomically consumes unique IDs and never resurrects uploaded consumed keys', async () => {
    expect((await post('/register-bundle', registration())).status).toBe(200);
    const get = () => fetch(url + '/get-bundle?userId=alice', { headers: { Authorization: 'Bearer bob' } }).then(r => r.json());
    const bundles = await Promise.all([get(), get(), get()]);
    expect(bundles.map(b => b.preKey?.keyId).filter(Boolean).sort()).toEqual([1, 2]);
    expect((await post('/register-bundle', registration())).status).toBe(200);
    expect((await post('/replenish-prekeys', { userId: 'alice', preKeys: registration().preKeys })).status).toBe(200);
    expect((await get()).preKey).toBeNull();
    expect((await get()).preKey).toBeNull();
    expect((await post('/register-bundle', registration('new-identity', [{ keyId: 1, publicKey: 'new-key' }]))).status).toBe(200);
    expect((await get()).preKey.publicKey).toBe('new-key');
  });
  it('does not overwrite a still-issued private-key ID', () => {
    const pool = { available: [{ keyId: 5, publicKey: 'old' }], consumed: [] };
    expect(() => mergePreKeys(pool, [{ keyId: 5, publicKey: 'new' }])).toThrow('conflict');
    const consumed = consumePreKey(pool).pool;
    expect(mergePreKeys(consumed, pool.available).available).toEqual([]);
  });
});
