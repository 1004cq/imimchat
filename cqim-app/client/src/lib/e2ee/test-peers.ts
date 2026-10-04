// Synthetic, ephemeral peers. No application database or real keys are read.
import { webcrypto } from 'node:crypto';
import { E2EEManager } from './E2EEManager';
import { exportKeyPair, generateKeyPair, generateSigningKeyPair, signSignedPreKey } from './CryptoUtils';

export async function peer(id: number) {
  Object.defineProperty(globalThis, 'crypto', { value: webcrypto, configurable: true });
  const manager = new (E2EEManager as any)() as E2EEManager;
  const identity = await exportKeyPair(await generateKeyPair());
  const signing = await exportKeyPair(await generateSigningKeyPair());
  const signed = await exportKeyPair(await generateKeyPair());
  const oneTime = await exportKeyPair(await generateKeyPair());
  let record: any;
  const keys = new Map([[7, { id: 7, keyPair: oneTime }]]);
  const store = {
    getSession: async () => record,
    saveSession: async (next: any) => { record = structuredClone(next); },
    removeSession: async () => { record = undefined; },
    getSignedPreKey: async (keyId: number) => keyId === 1 ? { id: 1, keyPair: signed } : undefined,
    getAllSignedPreKeys: async () => [{ id: 1, keyPair: signed }],
    getPreKey: async (keyId: number) => keys.get(keyId),
    removePreKey: async (keyId: number) => { keys.delete(keyId); },
    saveIdentity: async () => {},
    commitReceived: async (next: any, keyId?: number) => {
      record = structuredClone(next);
      if (keyId != null) keys.delete(keyId);
    },
  };
  Object.assign(manager, { store, _identityKeyPair: identity, _registrationId: id });
  const bundle = {
    registrationId: id, identityKey: identity.pubKey, signingPublicKey: signing.pubKey,
    signedPreKeyId: 1, signedPreKey: signed.pubKey,
    signedPreKeySignature: await signSignedPreKey(signing.privKey, signed.pubKey),
    oneTimePreKeyId: 7, oneTimePreKey: oneTime.pubKey,
  };
  return { manager, bundle, keys, state: () => record?.sessionData };
}
