import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';
import { spawn, spawnSync, type ChildProcessWithoutNullStreams } from 'node:child_process';
import { createInterface } from 'node:readline';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
vi.mock('@/lib/localdb', () => ({ clearDecryptedMessageCache: async () => {} }));
vi.mock('./WorkerProxy', () => ({ e2eeProxy: { isWorkerAvailable: false } }));
vi.mock('./SignalStore', () => ({ SignalStore: { shared: () => ({}) } }));
import { peer } from './test-peers';
import type { SignalEnvelope } from './E2EEManager';
let child: ChildProcessWithoutNullStreams, directory: string;
const pending: Array<(result: any) => void> = [];
const source = process.env.IMIM_NATIVE_E2EE_SOURCE;
const command = (op: string, user: string, extra: Record<string, unknown> = {}) => new Promise<any>(resolve => {
  pending.push(resolve); child.stdin.write(JSON.stringify({ op, user, ...extra }) + '\n');
});

describe.skipIf(!source || process.platform !== 'darwin')('production Swift / Web Crypto interoperability', () => {
  beforeAll(() => {
    directory = mkdtempSync(join(tmpdir(), 'imim-e2ee-test-'));
    const binary = join(directory, 'interop');
    const build = spawnSync('/usr/bin/xcrun', ['swiftc', '-swift-version', '5', '-D', 'E2EE_REGRESSION_TESTS', source!, 'scripts/e2ee/native-interop.swift', '-o', binary], { encoding: 'utf8' });
    if (build.status !== 0) throw new Error('Swift harness compile failed: ' + build.stderr);
    child = spawn(binary);
    createInterface({ input: child.stdout }).on('line', line => {
      if (line.startsWith('RESULT ')) pending.shift()?.(JSON.parse(line.slice(7)));
    });
  }, 60_000);
  afterAll(() => { child?.stdin.end(); child?.kill(); if (directory) rmSync(directory, { recursive: true }); });

  it('Web first message -> native, native reply -> Web, with second ratchet', async () => {
    const alice = await peer(101);
    const { bundle } = await command('init', 'native-bob');
    await alice.manager.establishSession('bob', bundle);
    const first = await alice.manager.encrypt('bob', JSON.stringify({ content: '你好 Swift', msgType: 'text' }));
    // Exercise deployed Web aliases and missing legacy version too.
    delete first.version; delete first.recipientSignedPreKeyId; delete first.recipientOneTimePreKeyId;
    expect(await command('decrypt', 'native-bob', { peer: 'alice', text: JSON.stringify(first) })).toEqual({ text: '你好 Swift' });
    const reply = JSON.parse((await command('encrypt', 'native-bob', { peer: 'alice', text: '你好 Web' })).envelope);
    expect(JSON.parse(await alice.manager.decrypt('bob', reply)).content).toBe('你好 Web');
    const next = await alice.manager.encrypt('bob', JSON.stringify({ content: 'next', msgType: 'text' }));
    expect((await command('decrypt', 'native-bob', { peer: 'alice', text: JSON.stringify(next) })).text).toBe('next');
  });

  it('native first message verifies the Web signed prekey and Web authenticates native signature', async () => {
    const bob = await peer(202);
    await command('init', 'native-alice');
    const first: SignalEnvelope = JSON.parse((await command('encrypt', 'native-alice', { peer: 'bob', text: 'native first', bundle: bob.bundle })).envelope);
    expect(JSON.parse(await bob.manager.decrypt('alice', first)).content).toBe('native first');
    const reply = await bob.manager.encrypt('alice', JSON.stringify({ content: 'web reply', msgType: 'text' }));
    expect((await command('decrypt', 'native-alice', { peer: 'bob', text: JSON.stringify(reply) })).text).toBe('web reply');
  });

  it('serializes the entire native read/derive/save operation across concurrent sends', async () => {
    const bob = await peer(505);
    await command('init', 'native-burst');
    const result = await command('encrypt_batch', 'native-burst', { peer: 'bob', bundle: bob.bundle });
    const messages: SignalEnvelope[] = result.envelopes.map((text: string) => JSON.parse(text));
    messages.sort((a, b) => a.counter - b.counter);
    expect(messages.map(m => m.counter)).toEqual([1, 2, 3]);
    const plaintexts = await Promise.all(messages.map(m => bob.manager.decrypt('alice', m)));
    expect(plaintexts.map(p => JSON.parse(p).content).sort()).toEqual(['burst-0', 'burst-1', 'burst-2']);
  });

  it('native preserves state after tampering, duplicate history and missing OPK, and accepts out-of-order messages', async () => {
    const alice = await peer(303);
    const { bundle } = await command('init', 'native-safe');
    await alice.manager.establishSession('bob', bundle);
    const first = await alice.manager.encrypt('bob', 'first');
    const bad = structuredClone(first); bad.ciphertext.ciphertext = 'AAAA';
    expect((await command('decrypt', 'native-safe', { peer: 'alice', text: JSON.stringify(bad) })).error).toBe(true);
    expect((await command('decrypt', 'native-safe', { peer: 'alice', text: JSON.stringify(first) })).text).toBe('first');
    const state = await command('state', 'native-safe', { peer: 'alice' });
    expect((await command('decrypt', 'native-safe', { peer: 'alice', text: JSON.stringify(first) })).error).toBe(true);
    expect(await command('state', 'native-safe', { peer: 'alice' })).toEqual(state);
    const missing = structuredClone(first); missing.senderRatchetKey = (await peer(404)).bundle.signedPreKey;
    missing.recipientOneTimePreKeyId = missing.usedOneTimePreKeyId = 999999;
    expect((await command('decrypt', 'native-safe', { peer: 'alice', text: JSON.stringify(missing) })).error).toBe(true);
    expect(await command('state', 'native-safe', { peer: 'alice' })).toEqual(state);
    const second = await alice.manager.encrypt('bob', 'second'), third = await alice.manager.encrypt('bob', 'third');
    expect((await command('decrypt', 'native-safe', { peer: 'alice', text: JSON.stringify(third) })).text).toBe('third');
    expect((await command('decrypt', 'native-safe', { peer: 'alice', text: JSON.stringify(second) })).text).toBe('second');
    expect((await command('decrypt', 'native-safe', { peer: 'alice', text: JSON.stringify(second) })).error).toBe(true);
  });
});
