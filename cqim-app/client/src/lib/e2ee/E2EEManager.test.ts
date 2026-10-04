import { beforeAll, describe, expect, it, vi } from 'vitest';
import { webcrypto } from 'node:crypto';
vi.mock('@/lib/localdb', () => ({ clearDecryptedMessageCache: async () => {} }));
vi.mock('./WorkerProxy', () => ({ e2eeProxy: { isWorkerAvailable: false } }));
vi.mock('./SignalStore', () => ({ SignalStore: { shared: () => ({}) } }));
import { E2EEManager, type SignalEnvelope } from './E2EEManager';
import { peer } from './test-peers';
import { assertValidSignedPreKey, exportKeyPair, generateSigningKeyPair } from './CryptoUtils';

beforeAll(() => { Object.defineProperty(globalThis, 'crypto', { value: webcrypto, configurable: true }); });

async function pair() {
  const alice = await peer(1), bob = await peer(2);
  await alice.manager.establishSession('bob', bob.bundle);
  return { alice, bob };
}

describe('authenticated E2EE lifecycle', () => {
  it('migrates a legacy signature without replacing an issued signed prekey', async () => {
    const alice = await peer(1);
    const manager = alice.manager as any, store = manager.store;
    const signing = await exportKeyPair(await generateSigningKeyPair());
    manager._signingKeyPair = signing;
    const original = { ...await store.getSignedPreKey(1), signature: 'legacy-public-key-string' };
    let saved: any;
    store.getLocalRegistration = async () => ({ registrationId: 1, signingKeyPair: signing });
    store.getAllSignedPreKeys = async () => [original];
    store.saveSignedPreKey = async (record: any) => { saved = record; };
    await manager.ensureSignedPreKeyMatchesSigningKey();
    expect(saved.id).toBe(original.id);
    expect(saved.keyPair).toEqual(original.keyPair);
    await assertValidSignedPreKey({ signingPublicKey: signing.pubKey, signedPreKey: saved.keyPair.pubKey, signedPreKeySignature: saved.signature });
  });

  it('never commits a conflicting native/Web OPK identifier', async () => {
    const { alice, bob } = await pair();
    const first = await alice.manager.encrypt('bob', 'first');
    first.recipientOneTimePreKeyId = 99;
    await expect(bob.manager.decrypt('alice', first)).rejects.toThrow('冲突');
    expect(bob.keys.size).toBe(1);
    expect(bob.state()).toBeUndefined();
  });
  it('round trips first messages, replies and a second ratchet', async () => {
    const { alice, bob } = await pair();
    expect(await bob.manager.decrypt('alice', await alice.manager.encrypt('bob', 'hello'))).toBe('hello');
    expect(bob.keys.size).toBe(0);
    expect(await alice.manager.decrypt('bob', await bob.manager.encrypt('alice', 'reply'))).toBe('reply');
    expect(await bob.manager.decrypt('alice', await alice.manager.encrypt('bob', 'next'))).toBe('next');
  });

  it('emits both native-v2 and deployed-Web prekey identifiers', async () => {
    const { alice, bob } = await pair();
    const message = await alice.manager.encrypt('bob', 'native aliases');
    expect(message.version).toBe(2);
    expect(message.recipientSignedPreKeyId).toBe(message.usedSignedPreKeyId);
    expect(message.recipientOneTimePreKeyId).toBe(message.usedOneTimePreKeyId);
    delete message.usedSignedPreKeyId;
    delete message.usedOneTimePreKeyId;
    expect(await bob.manager.decrypt('alice', message)).toBe('native aliases');
  });

  it('does not consume an OPK or persist a candidate on authentication failure', async () => {
    const { alice, bob } = await pair();
    const message = await alice.manager.encrypt('bob', 'valid');
    const tampered = structuredClone(message);
    tampered.ciphertext.ciphertext = 'AAAA';
    await expect(bob.manager.decrypt('alice', tampered)).rejects.toThrow();
    expect(bob.keys.size).toBe(1);
    expect(bob.state()).toBeUndefined();
    expect(await bob.manager.decrypt('alice', message)).toBe('valid');
  });

  it('rejects replays without rolling back a healthy session', async () => {
    const { alice, bob } = await pair();
    const first = await alice.manager.encrypt('bob', 'first');
    await bob.manager.decrypt('alice', first);
    const before = bob.state();
    await expect(bob.manager.decrypt('alice', first)).rejects.toThrow();
    expect(bob.state()).toBe(before);
    expect(await bob.manager.decrypt('alice', await alice.manager.encrypt('bob', 'second'))).toBe('second');
  });

  it('derives skipped keys for gaps and uses each exactly once', async () => {
    const { alice, bob } = await pair();
    await bob.manager.decrypt('alice', await alice.manager.encrypt('bob', 'first'));
    const second = await alice.manager.encrypt('bob', 'second');
    const third = await alice.manager.encrypt('bob', 'third');
    expect(await bob.manager.decrypt('alice', third)).toBe('third');
    expect(await bob.manager.decrypt('alice', second)).toBe('second');
    const before = bob.state();
    await expect(bob.manager.decrypt('alice', second)).rejects.toThrow();
    expect(bob.state()).toBe(before);
  });

  it('serializes concurrent sends and receives', async () => {
    const { alice, bob } = await pair();
    const messages = await Promise.all(['a', 'b', 'c'].map(text => alice.manager.encrypt('bob', text)));
    expect(messages.map(m => m.counter)).toEqual([1, 2, 3]);
    expect(await Promise.all(messages.map(m => bob.manager.decrypt('alice', m)))).toEqual(['a', 'b', 'c']);
  });

  it('rejects an excessive counter gap without committing state', async () => {
    const { alice, bob } = await pair();
    await bob.manager.decrypt('alice', await alice.manager.encrypt('bob', 'first'));
    const before = bob.state();
    const message = await alice.manager.encrypt('bob', 'next');
    message.counter = 1000000;
    await expect(bob.manager.decrypt('alice', message)).rejects.toThrow('安全窗口');
    expect(bob.state()).toBe(before);
  });
});
