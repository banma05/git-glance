import { describe, expect, it } from 'vitest';
import type { BranchView } from './types';
import { blockText, chips, dateTime, formatBytes, topChip } from './format';
import { makeT } from './i18n';

const base: BranchView = {
  name: 'feature/login', short: 'feature/login', topic: 'login', color: 0,
  trunk: 'main', trunkSource: 'auto', folder: '', folderHead: '', mainFolder: false, folderMissing: false,
  folderBroken: false, folderLocked: false, dirty: null, hasUpstream: true, upstreamGone: false, ahead: 0, behind: 0, unpushed: 0,
  behindTrunk: 0, own: [], forkHash: '', forkTime: 0, merged: false, test: null, release: null,
  canRemoveFolder: false, removeBlock: '', old: false, lastCommit: 0, error: '',
};
const t = makeT('en');
const keys = (b: Partial<BranchView>) => chips({ ...base, ...b }, t).map((c) => c.key);

describe('chips', () => {
  it('no upstream and unpushed commits, with 2 changes', () => {
    const c = chips({ ...base, hasUpstream: false, unpushed: 2, dirty: { changed: 1, untracked: 1, files: ['a', 'b'] } }, t);
    expect(c.map((x) => x.key)).toEqual(['unpushed', 'dirty']);
    expect(c[0].tone).toBe('mute');
    expect(c[1].tone).toBe('warn');
    expect(c[1].text).toContain('2');
  });
  it('behind the upstream by 3', () => {
    const c = chips({ ...base, behind: 3 }, t);
    expect(c.map((x) => x.key)).toEqual(['behind']);
    expect(c[0].tone).toBe('warn');
    expect(c[0].text).toContain('3');
  });
  it('ahead of the upstream, and in sync', () => {
    expect(keys({ ahead: 2 })).toEqual(['ahead']);
    expect(keys({})).toEqual(['synced']);
    expect(keys({ ahead: 1, behind: 1 })).toEqual(['behind', 'ahead']);
  });
  it('partly on the test branch 3/4', () => {
    const c = chips({ ...base, test: { state: 'some', matched: 3, total: 4, per: [] } }, t);
    expect(c.map((x) => x.key)).toEqual(['synced', 'test-some']);
    expect(c[1].tone).toBe('info');
    expect(c[1].text).toContain('3/4');
  });
  it('all and none on test', () => {
    expect(keys({ test: { state: 'all', matched: 4, total: 4, per: [] } })).toEqual(['synced', 'test-all']);
    expect(keys({ test: { state: 'none', matched: 0, total: 4, per: [] } })).toEqual(['synced', 'test-none']);
  });
  it('partly released 3/14', () => {
    const inn = ['a', 'b', 'c'];
    const notIn = Array.from({ length: 11 }, (_, i) => `r${i}`);
    const c = chips({ ...base, release: { state: 'some', in: inn, notIn } }, t);
    expect(c.map((x) => x.key)).toEqual(['synced', 'release-some']);
    expect(c[1].text).toContain('3/14');
    expect(keys({ release: { state: 'all', in: ['a'], notIn: [] } })).toEqual(['synced', 'release-all']);
    expect(keys({ release: { state: 'none', in: [], notIn: ['a'] } })).toEqual(['synced', 'release-none']);
  });
  it('merged and removable', () => {
    expect(keys({ merged: true, canRemoveFolder: true })).toEqual(['synced', 'removable']);
    expect(keys({ merged: true, canRemoveFolder: false })).toEqual(['synced']);
  });
  it('missing folder', () => {
    expect(keys({ folderMissing: true })).toEqual(['synced', 'folder-missing']);
  });
  it('a folder git cannot read', () => {
    const c = chips({ ...base, folderMissing: true, folderBroken: true }, t);
    expect(c.map((x) => x.key)).toEqual(['synced', 'folder-broken']);
    expect(c[1].text).toBe(t('chip.folderBroken'));
  });
  it('a branch whose details could not be read comes first and wins the graph row', () => {
    const c = chips({ ...base, error: 'fatal: bad object', behind: 2 }, t);
    expect(c[0]).toEqual({ key: 'error', text: t('chip.error'), tone: 'warn' });
    expect(topChip(c)?.key).toBe('error');
  });
  it('no chip about the upstream when it is gone', () => {
    expect(keys({ upstreamGone: true })).toEqual([]);
  });
});

describe('blockText', () => {
  it('maps every reason from the core and falls back to the raw value', () => {
    for (const b of ['not-in-folder', 'main-folder', 'folder-missing', 'locked-worktree', 'dirty', 'unpushed', 'not-merged', 'error']) {
      expect(blockText(b, t)).not.toBe(b);
    }
    expect(blockText('something-new', t)).toBe('something-new');
  });
});

describe('formatBytes and dateTime', () => {
  it('writes sizes in the largest unit', () => {
    expect(formatBytes(0)).toBe('0 B');
    expect(formatBytes(1536)).toBe('1.5 KB');
    expect(formatBytes(2048)).toBe('2 KB');
    expect(formatBytes(5 * 1024 * 1024)).toBe('5 MB');
  });
  it('leaves an unset time empty', () => {
    expect(dateTime(0)).toBe('');
    expect(dateTime(1700000000)).toMatch(/^\d{4}-\d\d-\d\d \d\d:\d\d$/);
  });
});

describe('topChip', () => {
  const pick = (b: Partial<BranchView>) => topChip(chips({ ...base, ...b }, t))?.key;
  const test = { state: 'all' as const, matched: 1, total: 1, per: [] };
  const release = { state: 'none' as const, in: [], notIn: ['main'] };

  it('prefers changes in the folder over everything else', () => {
    expect(pick({ dirty: { changed: 1, untracked: 0, files: [] }, behind: 2, folderMissing: true, test })).toBe('dirty');
  });
  it('follows the order dirty, behind, folder missing, removable, release, test, in sync', () => {
    expect(pick({ behind: 2, folderMissing: true, test })).toBe('behind');
    expect(pick({ folderMissing: true, test })).toBe('folder-missing');
    expect(pick({ merged: true, canRemoveFolder: true, test })).toBe('removable');
    expect(pick({ test, release })).toBe('release-none');
    expect(pick({ test })).toBe('test-all');
    expect(pick({})).toBe('synced');
  });
  it('falls back to what is left, and to null with no chips', () => {
    expect(pick({ hasUpstream: false, unpushed: 2 })).toBe('unpushed');
    expect(topChip([])).toBeNull();
  });
});
