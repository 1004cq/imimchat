import { generateKeyPairSync } from 'node:crypto';
import { describe, expect, it } from 'vitest';
import {
  filterValidP256PreKeys,
  isValidP256SPKIPublicKey,
  mergeValidP256PreKeys,
  normalizeConsumedPreKeyIds,
  rememberConsumedPreKey,
  resolveSigningPublicKey,
  takeOneValidP256PreKey,
} from './prekey-bundle.ts';

function p256SPKI(): string {
  const { publicKey } = generateKeyPairSync('ec', {
    namedCurve: 'prime256v1',
    publicKeyEncoding: { type: 'spki', format: 'der' },
    privateKeyEncoding: { type: 'pkcs8', format: 'der' },
  });
  return publicKey.toString('base64');
}

describe('register-bundle signingPublicKey merge', () => {
  it('uses the incoming ECDSA public key when present', () => {
    expect(resolveSigningPublicKey('new-sign', 'ik', JSON.stringify({
      identityKey: 'ik',
      signingPublicKey: 'old-sign',
    }))).toBe('new-sign');
  });

  it('keeps the stored signing key if an old client omits the field', () => {
    expect(resolveSigningPublicKey(undefined, 'ik', JSON.stringify({
      identityKey: 'ik',
      signingPublicKey: 'old-sign',
    }))).toBe('old-sign');
  });

  it('does not keep a signing key after identity rotation', () => {
    expect(resolveSigningPublicKey(undefined, 'new-ik', JSON.stringify({
      identityKey: 'old-ik',
      signingPublicKey: 'old-sign',
    }))).toBe(null);
  });
});

describe('P-256 PreKey validation', () => {
  it('accepts P-256 SPKI and rejects legacy 32-byte raw keys', () => {
    expect(isValidP256SPKIPublicKey(p256SPKI())).toBe(true);
    expect(isValidP256SPKIPublicKey(Buffer.alloc(32, 7).toString('base64'))).toBe(false);
  });

  it('filters invalid stored keys and keeps incoming replacement by key id', () => {
    const oldValid = p256SPKI();
    const replacement = p256SPKI();
    const incoming = p256SPKI();
    expect(filterValidP256PreKeys([
      { keyId: 1, publicKey: Buffer.alloc(32, 1).toString('base64') },
      { keyId: 2, publicKey: oldValid },
    ])).toEqual([{ keyId: 2, publicKey: oldValid }]);
    expect(mergeValidP256PreKeys(
      [{ keyId: 2, publicKey: oldValid }],
      [{ keyId: 2, publicKey: replacement }, { keyId: 3, publicKey: incoming }],
    )).toEqual([
      { keyId: 2, publicKey: replacement },
      { keyId: 3, publicKey: incoming },
    ]);
  });

  it('removes a delivered one-time key from the returned pool', () => {
    const first = { keyId: 7, publicKey: p256SPKI() };
    const second = { keyId: 8, publicKey: p256SPKI() };
    const taken = takeOneValidP256PreKey([first, second]);
    expect(taken.preKey).toEqual(first);
    expect(taken.remaining).toEqual([second]);
    expect(taken.remaining).not.toContainEqual(first);
  });

  it('returns no key for an empty or invalid pool', () => {
    expect(takeOneValidP256PreKey([])).toEqual({ preKey: null, remaining: [] });
    expect(takeOneValidP256PreKey([{ keyId: 1, publicKey: 'invalid' }]))
      .toEqual({ preKey: null, remaining: [] });
  });

  it('never merges an already consumed key back into the pool', () => {
    const consumed = { keyId: 21, publicKey: p256SPKI() };
    const fresh = { keyId: 22, publicKey: p256SPKI() };
    expect(mergeValidP256PreKeys([], [consumed, fresh], [21])).toEqual([fresh]);
    expect(takeOneValidP256PreKey([consumed, fresh], [21])).toEqual({
      preKey: fresh,
      remaining: [],
    });
  });

  it('deduplicates and remembers consumed key ids', () => {
    expect(normalizeConsumedPreKeyIds([1, 1, '2', null, 3])).toEqual([1, 3]);
    expect(rememberConsumedPreKey([1, 3], 4)).toEqual([1, 3, 4]);
  });
});
