import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import express from 'express';
import { createServer, type Server } from 'http';
import type { AddressInfo } from 'net';

const store = new Map<string, string>();

vi.mock('./redis.js', () => ({
  redis: {
    get: async (key: string) => store.get(key) ?? null,
    set: async (key: string, value: string) => {
      store.set(key, value);
      return 'OK';
    },
    del: async (...keys: string[]) => {
      for (const key of keys) store.delete(key);
      return keys.length;
    },
  },
}));

vi.mock('./auth.js', () => ({
  userAuth: (req: any, res: any, next: any) => {
    if (req.headers.authorization !== 'Bearer test-token') {
      return res.status(401).json({ error: '未登录' });
    }
    req.user = { id: 'user-1' };
    next();
  },
}));

import presenceRouter from './presence.ts';

async function listen(app: express.Express): Promise<{ server: Server; url: string }> {
  const server = createServer(app);
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const { port } = server.address() as AddressInfo;
  return { server, url: `http://127.0.0.1:${port}` };
}

describe('POST /api/presence', () => {
  let server: Server;
  let url: string;

  beforeEach(async () => {
    store.clear();
    const app = express();
    app.use(express.json());
    app.use('/api/presence', presenceRouter);
    ({ server, url } = await listen(app));
  });

  afterEach(async () => {
    await new Promise<void>((resolve, reject) => {
      server.close((err) => (err ? reject(err) : resolve()));
    });
  });

  it('requires userAuth bearer token', async () => {
    const res = await fetch(`${url}/api/presence`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ state: 'background' }),
    });
    expect(res.status).toBe(401);
  });

  it('stores background so APNs is not skipped due to WS', async () => {
    const res = await fetch(`${url}/api/presence`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer test-token',
      },
      body: JSON.stringify({ state: 'background' }),
    });
    expect(res.status).toBe(200);
    expect(await res.json()).toMatchObject({ success: true, state: 'background' });
    expect(store.get('user:presence:user-1')).toBe('background');

    const read = await fetch(`${url}/api/presence`, {
      headers: { Authorization: 'Bearer test-token' },
    });
    expect(await read.json()).toMatchObject({
      state: 'background',
      skipApns: false,
    });
  });

  it('rejects invalid state', async () => {
    const res = await fetch(`${url}/api/presence`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer test-token',
      },
      body: JSON.stringify({ state: 'online' }),
    });
    expect(res.status).toBe(400);
  });

  it('skips APNs only after foreground report', async () => {
    const res = await fetch(`${url}/api/presence`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer test-token',
      },
      body: JSON.stringify({ state: 'foreground', activeChatId: 'chat-1' }),
    });
    expect(res.status).toBe(200);
    const read = await fetch(`${url}/api/presence`, {
      headers: { Authorization: 'Bearer test-token' },
    });
    expect(await read.json()).toMatchObject({
      state: 'foreground',
      activeChatId: 'chat-1',
      skipApns: true,
    });
  });
});
