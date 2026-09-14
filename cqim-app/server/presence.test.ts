import { describe, it, expect } from 'vitest';
import { shouldSkipApnsFromState } from './presence-rules.ts';

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
