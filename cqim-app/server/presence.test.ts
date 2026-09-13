import { describe, it, expect } from 'vitest';
import { parsePresenceBody, shouldSkipApnsFromState } from './presence-rules.ts';

describe('shouldSkipApnsFromState', () => {
  it('never skips when background (WS online is irrelevant)', () => {
    expect(shouldSkipApnsFromState('background')).toBe(false);
    expect(shouldSkipApnsFromState('background', 'chat-1', 'chat-1')).toBe(false);
  });

  it('never skips when offline / TTL expired', () => {
    expect(shouldSkipApnsFromState('offline', 'chat-1', 'chat-1')).toBe(false);
  });

  it('skips group/no-chatId pushes while foreground', () => {
    expect(shouldSkipApnsFromState('foreground')).toBe(true);
    expect(shouldSkipApnsFromState('foreground', undefined, 'chat-1')).toBe(true);
  });

  it('skips all DMs when foreground but no active chat', () => {
    expect(shouldSkipApnsFromState('foreground', 'chat-1', null)).toBe(true);
    expect(shouldSkipApnsFromState('foreground', 'chat-1', '')).toBe(true);
  });

  it('skips only the open DM when foreground + activeChatId', () => {
    expect(shouldSkipApnsFromState('foreground', 'chat-1', 'chat-1')).toBe(true);
    expect(shouldSkipApnsFromState('foreground', 'chat-2', 'chat-1')).toBe(false);
  });
});

describe('parsePresenceBody', () => {
  it('rejects missing or invalid state', () => {
    expect(parsePresenceBody({})).toEqual({
      ok: false,
      error: 'state 必须是 foreground、background 或 offline',
    });
    expect(parsePresenceBody({ state: 'online' }).ok).toBe(false);
  });

  it('accepts background without treating WS as skip', () => {
    expect(parsePresenceBody({ state: 'background' })).toEqual({
      ok: true,
      state: 'background',
    });
  });

  it('normalizes empty activeChatId to null', () => {
    expect(parsePresenceBody({ state: 'foreground', activeChatId: '' })).toEqual({
      ok: true,
      state: 'foreground',
      activeChatId: null,
    });
    expect(parsePresenceBody({ state: 'foreground', activeChatId: 'chat-1' })).toEqual({
      ok: true,
      state: 'foreground',
      activeChatId: 'chat-1',
    });
  });
});
