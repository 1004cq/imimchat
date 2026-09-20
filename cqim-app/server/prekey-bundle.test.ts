import { describe, expect, it } from 'vitest';
import { resolveSigningPublicKey } from './prekey-bundle.ts';

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
