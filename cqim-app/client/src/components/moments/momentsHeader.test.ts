import { describe, expect, it } from 'vitest';
import {
  MOMENTS_HEADER_AVATAR_ATTR,
  MOMENTS_HEADER_AVATAR_SELECTOR,
  shouldOpenMomentsCoverPicker,
} from './utils';

function fakeTarget(matchesAvatar: boolean) {
  return {
    closest: (selector: string) => (matchesAvatar && selector === MOMENTS_HEADER_AVATAR_SELECTOR ? {} : null),
  };
}

describe('shouldOpenMomentsCoverPicker', () => {
  it('opens the cover picker for a tap on the header/cover itself', () => {
    expect(shouldOpenMomentsCoverPicker(fakeTarget(false))).toBe(true);
    expect(shouldOpenMomentsCoverPicker(null)).toBe(true);
  });

  it('does not open the cover picker for a tap on the header avatar', () => {
    expect(shouldOpenMomentsCoverPicker(fakeTarget(true))).toBe(false);
  });

  it('keeps the avatar marker in sync with the selector', () => {
    expect(MOMENTS_HEADER_AVATAR_SELECTOR).toBe(`[${MOMENTS_HEADER_AVATAR_ATTR}]`);
  });
});
