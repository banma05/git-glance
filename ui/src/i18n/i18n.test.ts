import { describe, expect, it } from 'vitest';
import { zh } from './zh-CN';
import { en } from './en';
import { makeT, pickLocale } from './index';

describe('dictionaries', () => {
  it('have identical keys', () => {
    expect(Object.keys(zh).sort()).toEqual(Object.keys(en).sort());
  });
  it('have no empty strings', () => {
    for (const d of [zh, en]) {
      for (const [k, v] of Object.entries(d)) expect(v, k).not.toBe('');
    }
  });
  it('use the same placeholders on both sides', () => {
    const names = (s: string) => (s.match(/\{\w+\}/g) ?? []).sort();
    for (const k of Object.keys(zh) as (keyof typeof zh)[]) {
      expect(names(en[k]), k).toEqual(names(zh[k]));
    }
  });
});

describe('locale', () => {
  it('picks Chinese for zh prefixes and English otherwise', () => {
    expect(pickLocale('zh-cn')).toBe('zh');
    expect(pickLocale('zh-TW')).toBe('zh');
    expect(pickLocale('en')).toBe('en');
    expect(pickLocale('de')).toBe('en');
    expect(pickLocale('')).toBe('en');
  });
  it('fills placeholders', () => {
    expect(makeT('en')('chip.behind', { n: 3 })).toContain('3');
    expect(makeT('zh-cn')('chip.behind', { n: 3 })).toContain('3');
  });
});
