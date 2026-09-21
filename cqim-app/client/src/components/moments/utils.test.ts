import { describe, expect, it } from 'vitest';
import { convertApiMoment } from './utils.ts';

describe('convertApiMoment authorAvatar', () => {
  it('keeps authorAvatar when present', () => {
    const item = convertApiMoment({
      id: 'm1',
      authorId: '1',
      authorName: 'Qing',
      authorAvatar: '/api/media/0670b9a72a7f207e7fb977f2',
      user: { avatar: '/api/media/stale' },
    });
    expect(item.authorAvatar).toBe('/api/media/0670b9a72a7f207e7fb977f2');
  });

  it('falls back to nested user.avatar when authorAvatar is empty', () => {
    const item = convertApiMoment({
      id: 'm2',
      authorId: '1',
      authorName: 'Qing',
      authorAvatar: '',
      user: { avatar: '/api/media/0670b9a72a7f207e7fb977f2' },
    });
    expect(item.authorAvatar).toBe('/api/media/0670b9a72a7f207e7fb977f2');
  });

  it('uses empty string when both avatars are missing so DoveAvatar can show initials', () => {
    const item = convertApiMoment({
      id: 'm3',
      authorId: '0',
      authorName: 'User0',
    });
    expect(item.authorAvatar).toBe('');
  });
});
