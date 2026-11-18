import { afterEach, describe, expect, it } from 'vitest';
import { mockHost, mockView } from './mock';
import { pickHost } from './types';

describe('mock host', () => {
  afterEach(() => history.replaceState(null, '', '/'));

  it('is chosen by ?mock=1', () => {
    history.replaceState(null, '', '/?mock=1');
    expect(pickHost().locale).toBe(mockHost().locale);
    return expect(pickHost().call('repos')).resolves.toEqual(expect.any(Array));
  });

  it('has no push, pull, sync, merge or rebase text', () => {
    const strings: string[] = [];
    JSON.stringify(mockView('/demo/repo'), (_k, v) => {
      if (typeof v === 'string') strings.push(v);
      return v;
    });
    // "unpushed" is a core reason code and "merge" a core graph row kind, not actions
    const text = strings.filter((x) => x !== 'merge').join(' | ');
    expect(text).not.toMatch(/(?<!un)push|pull|sync|merge|rebase/i);
  });

  it('covers a dirty folder, a missing folder and an old branch', () => {
    const v = mockView('/demo/repo');
    const all = Object.values(v.branches);
    expect(all.some((b) => b.dirty)).toBe(true);
    expect(all.some((b) => b.folderMissing)).toBe(true);
    expect(v.old.length).toBeGreaterThan(0);
    expect(v.groups.map((g) => g.trunk.name)).toEqual(['main', 'develop']);
  });
});

describe('mock graph data', () => {
  it('uses realistic 40-digit hashes, not zero padding', () => {
    const v = mockView('/demo/repo');
    const hashes = v.sections.flatMap((s) => s.rows.map((r) => r.hash)).filter(Boolean);
    expect(hashes.length).toBeGreaterThan(0);
    for (const h of hashes) {
      expect(h).toMatch(/^[0-9a-f]{40}$/);
      expect(h.startsWith('0000000')).toBe(false);
    }
    expect(new Set(hashes).size).toBeGreaterThan(8);
  });

  it('gives several branches their own commits on lanes beside the trunk', () => {
    const v = mockView('/demo/repo');
    const withOwn = Object.values(v.branches).filter((b) => b.own.length > 0 && !b.merged);
    expect(withOwn.length).toBeGreaterThanOrEqual(2);
    const lanes = new Set(
      v.sections.flatMap((s) => s.rows.filter((r) => r.kind === 'commit').map((r) => r.lane)),
    );
    expect(Math.min(...lanes)).toBeGreaterThanOrEqual(1);
    expect(lanes.size).toBeGreaterThanOrEqual(2);
  });
});
