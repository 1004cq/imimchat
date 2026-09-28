import { describe, expect, it } from 'vitest';
import {
  resolvePreKeyIds,
  isPeerRebuilt,
  type SignalEnvelope,
} from './E2EEManager.ts';

function baseEnvelope(): SignalEnvelope {
  return {
    type: 'prekey',
    senderRegistrationId: 1,
    senderIdentityKey: 'aWQ=',
    senderRatchetKey: 'cmt=',
    previousCounter: 0,
    counter: 1,
    ciphertext: { ciphertext: 'Yw==', iv: 'aXY=', tag: '' },
    timestamp: Date.now(),
  };
}

/**
 * iOS 互操作回归：iOS 端 prekey 信封使用 recipient*PreKeyId 字段名，
 * Web 必须能解析；Web 发送时双写，保证 iOS 也能解析。
 */
describe('resolvePreKeyIds (iOS field-name compat)', () => {
  it('reads used* fields (Web-to-Web)', () => {
    const env = baseEnvelope();
    env.usedSignedPreKeyId = 7;
    env.usedOneTimePreKeyId = 42;
    expect(resolvePreKeyIds(env)).toEqual({ signedPreKeyId: 7, oneTimePreKeyId: 42 });
  });

  it('reads recipient* fields (iOS-to-Web)', () => {
    const env = baseEnvelope();
    env.recipientSignedPreKeyId = 7;
    env.recipientOneTimePreKeyId = 42;
    expect(resolvePreKeyIds(env)).toEqual({ signedPreKeyId: 7, oneTimePreKeyId: 42 });
  });

  it('prefers used* when both present', () => {
    const env = baseEnvelope();
    env.usedSignedPreKeyId = 7;
    env.recipientSignedPreKeyId = 9;
    env.usedOneTimePreKeyId = 42;
    env.recipientOneTimePreKeyId = 43;
    expect(resolvePreKeyIds(env)).toEqual({ signedPreKeyId: 7, oneTimePreKeyId: 42 });
  });

  it('returns undefined when neither present', () => {
    expect(resolvePreKeyIds(baseEnvelope())).toEqual({
      signedPreKeyId: undefined,
      oneTimePreKeyId: undefined,
    });
  });

  it('handles partial presence (signed only)', () => {
    const env = baseEnvelope();
    env.recipientSignedPreKeyId = 7;
    expect(resolvePreKeyIds(env)).toEqual({ signedPreKeyId: 7, oneTimePreKeyId: undefined });
  });
});

/**
 * 对端重建检测回归：一方丢会话重发 prekey 时，
 * 另一方必须重建（否则双向永久无法解密）；
 * 重复/延迟的旧 prekey 必须不重建。
 */
describe('isPeerRebuilt', () => {
  it('returns true when peer rebuilt with a new ephemeral key', () => {
    expect(isPeerRebuilt('chainKey', 'oldEphemeral', 'newEphemeral')).toBe(true);
  });

  it('returns false for duplicate prekey with the same ephemeral key', () => {
    expect(isPeerRebuilt('chainKey', 'sameEphemeral', 'sameEphemeral')).toBe(false);
  });

  it('returns true when we were initiator (no stored ephemeral key)', () => {
    // 发起方建的会话没有 lastPrekeyEphemeralKey；收到 prekey 即对端新发起
    expect(isPeerRebuilt('chainKey', undefined, 'peerEphemeral')).toBe(true);
  });

  it('returns false when local session has no receive chain (not established)', () => {
    expect(isPeerRebuilt(null, 'oldEphemeral', 'newEphemeral')).toBe(false);
    expect(isPeerRebuilt(undefined, 'oldEphemeral', 'newEphemeral')).toBe(false);
    expect(isPeerRebuilt('', 'oldEphemeral', 'newEphemeral')).toBe(false);
  });

  it('returns false when envelope has no ephemeral key', () => {
    expect(isPeerRebuilt('chainKey', 'oldEphemeral', undefined)).toBe(false);
  });
});
