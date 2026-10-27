import { describe, expect, it } from 'vitest';
import { authenticateSignal, sessionResetSignal } from './signal-auth';
const valid = async () => ({ userId: 'alice', expiresAt: new Date(Date.now() + 60_000), user: { isBanned: false } });
describe('authenticated signal identity', () => {
  it('binds the connection to the authenticated session, not the URL claim', async () => {
    expect(await authenticateSignal('test-token', 'alice', valid)).toBe('alice');
    expect(await authenticateSignal('test-token', null, valid)).toBe('alice');
    await expect(authenticateSignal('test-token', 'bob', valid)).rejects.toThrow();
  });
  it('fails closed on absent, expired, banned and unverifiable sessions', async () => {
    await expect(authenticateSignal(null, 'alice', valid)).rejects.toThrow();
    await expect(authenticateSignal('test-token', 'alice', async () => null)).rejects.toThrow();
    await expect(authenticateSignal('test-token', 'alice', async () => ({ ...await valid(), expiresAt: new Date(0) }))).rejects.toThrow();
    await expect(authenticateSignal('test-token', 'alice', async () => ({ ...await valid(), user: { isBanned: true } }))).rejects.toThrow();
    await expect(authenticateSignal('test-token', 'alice', async () => { throw new Error('DB unavailable'); })).rejects.toThrow();
  });
  it('forwards explicit recovery only to an existing private-chat peer', async () => {
    expect(await sessionResetSignal('alice', 'bob', async () => true)).toEqual({ type: 'e2ee_session_reset', from: 'alice', to: 'bob' });
    expect(await sessionResetSignal('alice', 'stranger', async () => false)).toBeNull();
    expect(await sessionResetSignal('alice', 'alice', async () => true)).toBeNull();
  });
});
