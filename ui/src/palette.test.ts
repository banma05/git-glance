import { describe, expect, it } from 'vitest';
import { BG, PALETTE, contrast } from './palette';

describe('palette', () => {
  for (const mode of ['dark', 'light'] as const) {
    it(`${mode}: 12 distinct colors`, () => {
      expect(PALETTE[mode]).toHaveLength(12);
      expect(new Set(PALETTE[mode].map((c) => c.toLowerCase())).size).toBe(12);
    });
    it(`${mode}: every color has contrast >= 3 on its background`, () => {
      for (const c of PALETTE[mode]) expect(contrast(c, BG[mode]), c).toBeGreaterThanOrEqual(3);
    });
  }
  it('computes the WCAG ratio', () => {
    expect(contrast('#000000', '#ffffff')).toBeCloseTo(21, 5);
    expect(contrast('#ffffff', '#ffffff')).toBeCloseTo(1, 5);
  });
});
