import { describe, expect, it } from 'vitest';
import { nextPreKeyStart } from './prekeyIds.ts';

describe('nextPreKeyStart', () => {
  it('starts at 1 when the device has no prekeys', () => {
    expect(nextPreKeyStart([], 1)).toBe(1);
  });

  it('does not reuse ids that wrapped through a clock modulo', () => {
    expect(nextPreKeyStart([1, 2, 19, 80019], 100)).toBe(80020);
    expect(nextPreKeyStart([80019], 1)).toBe(80020);
  });
});
