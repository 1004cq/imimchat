import { describe, expect, it } from 'vitest';
import {
  mergeAndCapPreKeys,
  resolveSigningPublicKey,
  selectPreKeyForIssue,
} from './prekey-bundle.ts';

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

describe('one-time prekey issue and replenish', () => {
  const keys = [
    { keyId: 1, publicKey: 'a' },
    { keyId: 2, publicKey: 'b' },
    { keyId: 3, publicKey: 'c' },
  ];

  it('issues the oldest available prekey and remembers it as consumed', () => {
    const plan = selectPreKeyForIssue(keys, []);
    expect(plan.taken).toEqual({ keyId: 1, publicKey: 'a' });
    expect(plan.rest.map(key => key.keyId)).toEqual([2, 3]);
    expect(plan.consumed).toEqual([1]);
  });

  it('does not reissue or resurrect a consumed prekey when the client uploads it again', () => {
    const first = selectPreKeyForIssue(keys, []);
    const republished = mergeAndCapPreKeys(first.rest, keys, first.consumed);
    expect(republished.map(key => key.keyId)).toEqual([2, 3]);

    const second = selectPreKeyForIssue(republished, first.consumed);
    expect(second.taken?.keyId).toBe(2);
    expect(second.rest.map(key => key.keyId)).toEqual([3]);
  });

  it('keeps the highest key ids when the inventory exceeds the cap', () => {
    const existing = Array.from({ length: 100 }, (_, index) => ({ keyId: index + 1, publicKey: 'old' }));
    const incoming = [{ keyId: 101, publicKey: 'new' }];
    const merged = mergeAndCapPreKeys(existing, incoming, [], 100);
    expect(merged[0].keyId).toBe(2);
    expect(merged[merged.length - 1]).toEqual({ keyId: 101, publicKey: 'new' });
    expect(merged.some(key => key.keyId === 1)).toBe(false);
  });
});
