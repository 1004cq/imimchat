import { describe, expect, it } from 'vitest';
import {
  generateKeyPair,
  exportKeyPair,
  importPublicKey,
  importPrivateKey,
  ecdh,
  hkdf,
  deriveMessageKeys,
  aesEncrypt,
  aesDecrypt,
  concatBuffers,
  stringToBuffer,
  bufferToString,
  bufferToBase64,
  base64ToBuffer,
} from './CryptoUtils.ts';

/**
 * 纯密码学回归：发起方 X3DH + 首次 imim-chain 棘轮
 * 必须与响应方对偶计算得到相同 root/chain，且首条密文可解。
 */
describe('X3DH initiator/responder symmetry', () => {
  it('Bob as responder derives the same first receive chain Alice used to encrypt', async () => {
    const aliceIK = await generateKeyPair();
    const bobIK = await generateKeyPair();
    const bobSPK = await generateKeyPair();
    const bobOPK = await generateKeyPair();
    const aliceEK = await generateKeyPair();
    const aliceRatchet = await generateKeyPair();

    const aliceIKPriv = aliceIK.privateKey;
    const aliceEKPriv = aliceEK.privateKey;
    const bobIKPub = bobIK.publicKey;
    const bobSPKPub = bobSPK.publicKey;
    const bobOPKPub = bobOPK.publicKey;

    // Alice (initiator) X3DH
    const aDH1 = await ecdh(aliceIKPriv, bobSPKPub);
    const aDH2 = await ecdh(aliceEKPriv, bobIKPub);
    const aDH3 = await ecdh(aliceEKPriv, bobSPKPub);
    const aDH4 = await ecdh(aliceEKPriv, bobOPKPub);
    const aDH = concatBuffers(aDH1, aDH2, aDH3, aDH4);

    const salt = new Uint8Array(32).buffer;
    const x3dhInfo = stringToBuffer('imim-x3dh');
    const aRoot = await hkdf(aDH, salt, x3dhInfo, 32);

    const aDhSend = await ecdh(aliceRatchet.privateKey, bobSPKPub);
    const chainInfo = stringToBuffer('imim-chain');
    const aDerived = await hkdf(concatBuffers(aRoot, aDhSend), salt, chainInfo, 64);
    const aNewRoot = aDerived.slice(0, 32);
    const aSendChain = aDerived.slice(32, 64);

    const { messageKey: aMsgKey, nextChainKey: aNext } = await deriveMessageKeys(aSendChain);
    const plaintext = 'hello-from-alice';
    const enc = await aesEncrypt(stringToBuffer(plaintext), aMsgKey);

    // Bob (responder) X3DH — dual of Alice
    const bobIKPriv = bobIK.privateKey;
    const bobSPKPriv = bobSPK.privateKey;
    const bobOPKPriv = bobOPK.privateKey;
    const aliceIKPub = aliceIK.publicKey;
    const aliceEKPub = aliceEK.publicKey;

    const bDH1 = await ecdh(bobSPKPriv, aliceIKPub);
    const bDH2 = await ecdh(bobIKPriv, aliceEKPub);
    const bDH3 = await ecdh(bobSPKPriv, aliceEKPub);
    const bDH4 = await ecdh(bobOPKPriv, aliceEKPub);
    const bDH = concatBuffers(bDH1, bDH2, bDH3, bDH4);
    const bRoot = await hkdf(bDH, salt, x3dhInfo, 32);

    expect(bufferToBase64(bRoot)).toBe(bufferToBase64(aRoot));

    const aliceRatchetPub = aliceRatchet.publicKey;
    const bDhRecv = await ecdh(bobSPKPriv, aliceRatchetPub);
    const bDerived = await hkdf(concatBuffers(bRoot, bDhRecv), salt, chainInfo, 64);
    const bNewRoot = bDerived.slice(0, 32);
    const bRecvChain = bDerived.slice(32, 64);

    expect(bufferToBase64(bNewRoot)).toBe(bufferToBase64(aNewRoot));
    expect(bufferToBase64(bRecvChain)).toBe(bufferToBase64(aSendChain));

    const { messageKey: bMsgKey } = await deriveMessageKeys(bRecvChain);
    expect(bufferToBase64(bMsgKey)).toBe(bufferToBase64(aMsgKey));

    const dec = await aesDecrypt(enc, bMsgKey);
    expect(bufferToString(dec)).toBe(plaintext);

    // 确保 next chain 也一致（后续 message 类型）
    const { nextChainKey: bNext } = await deriveMessageKeys(bRecvChain);
    expect(bufferToBase64(bNext)).toBe(bufferToBase64(aNext));
  });

  it('old broken handlePreKeyMessage path (initiator-as-responder) does NOT match', async () => {
    // 回归：若 Bob 错误地再跑一遍发起方 X3DH（用 Alice 的 bundle），共享密钥必然不同
    const aliceIK = await exportKeyPair(await generateKeyPair());
    const bobIK = await exportKeyPair(await generateKeyPair());
    const bobSPK = await exportKeyPair(await generateKeyPair());
    const aliceEK = await generateKeyPair();

    const aliceIKPriv = await importPrivateKey(aliceIK.privKey);
    const bobSPKPub = await importPublicKey(bobSPK.pubKey);
    const bobIKPub = await importPublicKey(bobIK.pubKey);

    const aDH1 = await ecdh(aliceIKPriv, bobSPKPub);
    const aDH2 = await ecdh(aliceEK.privateKey, bobIKPub);
    const aDH3 = await ecdh(aliceEK.privateKey, bobSPKPub);
    const salt = new Uint8Array(32).buffer;
    const info = stringToBuffer('imim-x3dh');
    const aRoot = await hkdf(concatBuffers(aDH1, aDH2, aDH3), salt, info, 32);

    // Bob wrongly acts as initiator against "Alice bundle" (Alice IK + some SPK)
    const aliceFakeSPK = await generateKeyPair();
    const bobIKPriv = await importPrivateKey(bobIK.privKey);
    const aliceIKPub = await importPublicKey(aliceIK.pubKey);
    const bobEK = await generateKeyPair();
    const wrongDH1 = await ecdh(bobIKPriv, aliceFakeSPK.publicKey);
    const wrongDH2 = await ecdh(bobEK.privateKey, aliceIKPub);
    const wrongDH3 = await ecdh(bobEK.privateKey, aliceFakeSPK.publicKey);
    const wrongRoot = await hkdf(concatBuffers(wrongDH1, wrongDH2, wrongDH3), salt, info, 32);

    expect(bufferToBase64(wrongRoot)).not.toBe(bufferToBase64(aRoot));
  });
});
