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
