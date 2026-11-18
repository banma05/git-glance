import * as fs from 'node:fs';
import * as path from 'node:path';
import { describe, expect, it } from 'vitest';

const root = path.resolve(__dirname, '..');
const load = (f: string) => JSON.parse(fs.readFileSync(path.join(root, 'l10n', f), 'utf8')) as Record<string, string>;

describe('l10n bundles', () => {
  const en = load('bundle.l10n.json');
  const zh = load('bundle.l10n.zh-cn.json');

  it('zh-cn has the same keys as the default bundle', () => {
    expect(Object.keys(zh).sort()).toEqual(Object.keys(en).sort());
  });

  it('every l10n.t call in the sources has a key', () => {
    const used = new Set<string>();
    for (const f of fs.readdirSync(path.join(root, 'src'))) {
      if (!f.endsWith('.ts') || f.endsWith('.test.ts')) continue;
      const src = fs.readFileSync(path.join(root, 'src', f), 'utf8');
      for (const m of src.matchAll(/l10n\.t\(\s*'((?:[^'\\]|\\.)*)'/g)) used.add(m[1].replace(/\\'/g, "'"));
    }
    expect(used.size).toBeGreaterThan(5);
    for (const k of used) expect(Object.keys(en), k).toContain(k);
  });

  it('the manifest points at the bundle folder', () => {
    expect(JSON.parse(fs.readFileSync(path.join(root, 'package.json'), 'utf8')).l10n).toBe('./l10n');
  });
});
