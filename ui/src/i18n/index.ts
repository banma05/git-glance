import { zh } from './zh-CN';
import { en } from './en';
import type { Key } from './en';

export type { Key };
export type T = (key: Key, vars?: Record<string, string | number>) => string;

export function pickLocale(locale: string): 'zh' | 'en' {
  return locale.toLowerCase().startsWith('zh') ? 'zh' : 'en';
}

// makeT returns the translator for an editor locale such as "zh-cn" or "en".
export function makeT(locale: string): T {
  const dict: Record<Key, string> = pickLocale(locale) === 'zh' ? zh : en;
  return (key, vars) => {
    const text = dict[key] ?? key;
    if (!vars) return text;
    return text.replace(/\{(\w+)\}/g, (whole, name: string) => (name in vars ? String(vars[name]) : whole));
  };
}
