import { describe, expect, it } from 'vitest';
import {
  generateKeyPair,
  generateSigningKeyPair,
  exportKeyPair,
  importPublicKey,
  importPrivateKey,
  ecdh,
  signSignedPreKey,
  verifySignedPreKeySignature,
  assertValidSignedPreKey,
  SIGNED_PREKEY_MISSING_SIGNING_KEY,
  SIGNED_PREKEY_SIGNATURE_INVALID,
  verify,
  base64ToBuffer,
} from './CryptoUtils.ts';

async function makePeerBundle() {
  const identity = await exportKeyPair(await generateKeyPair());
  const signing = await exportKeyPair(await generateSigningKeyPair());
  const signedPreKey = await exportKeyPair(await generateKeyPair());
  const signedPreKeySignature = await signSignedPreKey(signing.privKey, signedPreKey.pubKey);
  return { identity, signing, signedPreKey, signedPreKeySignature };
}

describe('Signed PreKey ECDSA vs ECDH identity key', () => {
  it('verifies a Signed PreKey with the ECDSA signing public key', async () => {
    const { signing, signedPreKey, signedPreKeySignature } = await makePeerBundle();
    await expect(
      verifySignedPreKeySignature(signing.pubKey, signedPreKey.pubKey, signedPreKeySignature),
    ).resolves.toBe(true);
    await expect(
      assertValidSignedPreKey({
        signingPublicKey: signing.pubKey,
        signedPreKey: signedPreKey.pubKey,
        signedPreKeySignature,
      }),
    ).resolves.toBeUndefined();
  });

  it('rejects the production bug: verifying against ECDH identityKey', async () => {
    const { identity, signedPreKey, signedPreKeySignature } = await makePeerBundle();

    const identityAsEcdsa = await importPublicKey(identity.pubKey, 'ECDSA');
    const importedOk = await verify(
      identityAsEcdsa,
      base64ToBuffer(signedPreKeySignature),
      base64ToBuffer(signedPreKey.pubKey),
    );
    expect(importedOk).toBe(false);

    await expect(
      verifySignedPreKeySignature(identity.pubKey, signedPreKey.pubKey, signedPreKeySignature),
    ).resolves.toBe(false);
    await expect(
      assertValidSignedPreKey({
        signingPublicKey: identity.pubKey,
        signedPreKey: signedPreKey.pubKey,
        signedPreKeySignature,
      }),
    ).rejects.toThrow(SIGNED_PREKEY_SIGNATURE_INVALID);
  });

  it('fails closed when the peer bundle has no signingPublicKey', async () => {
    const { signedPreKey, signedPreKeySignature } = await makePeerBundle();
    await expect(
      assertValidSignedPreKey({
        signedPreKey: signedPreKey.pubKey,
        signedPreKeySignature,
      }),
    ).rejects.toThrow(SIGNED_PREKEY_MISSING_SIGNING_KEY);
  });

  it('keeps identityKey usable for X3DH ECDH after signature verification', async () => {
    const aliceIdentity = await generateKeyPair();
    const aliceEph = await generateKeyPair();
    const { identity, signing, signedPreKey, signedPreKeySignature } = await makePeerBundle();

    await assertValidSignedPreKey({
      signingPublicKey: signing.pubKey,
      signedPreKey: signedPreKey.pubKey,
      signedPreKeySignature,
    });

    const bobIdentityPub = await importPublicKey(identity.pubKey, 'ECDH');
    const bobSpkPub = await importPublicKey(signedPreKey.pubKey, 'ECDH');

    const dh1 = await ecdh(aliceIdentity.privateKey, bobSpkPub);
    const dh2 = await ecdh(aliceEph.privateKey, bobIdentityPub);
    const dh3 = await ecdh(aliceEph.privateKey, bobSpkPub);

    expect(dh1.byteLength).toBe(32);
    expect(dh2.byteLength).toBe(32);
    expect(dh3.byteLength).toBe(32);

    const bobIdentityPriv = await importPrivateKey(identity.privKey, 'ECDH');
    const aliceEphPub = aliceEph.publicKey;
    const dh2Mirror = await ecdh(bobIdentityPriv, aliceEphPub);
    expect(Buffer.from(dh2Mirror).equals(Buffer.from(dh2))).toBe(true);
  });
});
