import type { BranchView, CommitInfo, Row, Section, View } from '../types';

// The rows of core/internal/graph TestLayout, transcribed. Times are unix seconds.
const row = (kind: Row['kind'], lane: number, branch: string, hash: string, time: number, count = 0, ends: number[] = []): Row => ({
  kind,
  lane,
  branch,
  hash,
  time,
  count,
  ends,
});

export const layoutSection: Section = {
  trunk: 'main',
  lanes: 4,
  rows: [
    row('head', 0, '', 'H', 1_700_000_100),
    row('commit', 1, 'A', 'a2', 1_700_000_099),
    row('commit', 1, 'A', 'a1', 1_700_000_098),
    row('commit', 2, 'B', 'b1', 1_700_000_097),
    row('commit', 3, 'C', 'c5', 1_700_000_096),
    row('more-own', 3, 'C', '', 1_700_000_095, 4),
    row('more', 0, '', '', 1_700_000_090, 6),
    row('fork', 0, '', 'F1', 1_700_000_090, 0, [1, 2]),
    row('merge', 1, 'D', '', 1_700_000_085, 2),
    row('more', 0, '', '', 1_700_000_080, 3),
    row('fork', 0, '', 'F2', 1_700_000_080, 0, [3]),
  ],
};

export function branchOf(name: string, own: CommitInfo[], over: Partial<BranchView> = {}): BranchView {
  return {
    name,
    short: name,
    topic: name,
    color: 0,
    trunk: 'main',
    trunkSource: 'count',
    folder: '',
    folderHead: '',
    mainFolder: false,
    folderMissing: false,
    folderBroken: false,
    folderLocked: false,
    dirty: null,
    hasUpstream: true,
    upstreamGone: false,
    ahead: 0,
    behind: 0,
    unpushed: 0,
    behindTrunk: 0,
    own,
    forkHash: 'F1',
    forkTime: 1_700_000_090,
    merged: false,
    test: null,
    release: null,
    canRemoveFolder: false,
    removeBlock: 'not-in-folder',
    old: false,
    lastCommit: 0,
    error: '',
    ...over,
  };
}

const ci = (hash: string, time: number): CommitInfo => ({ hash, subject: 'commit ' + hash, time });

export const layoutBranches: Record<string, BranchView> = {
  A: branchOf('A', [ci('a2', 1_700_000_099), ci('a1', 1_700_000_098)], { color: 0, folder: '/w/wt-a' }),
  B: branchOf('B', [ci('b1', 1_700_000_097)], { color: 1 }),
  C: branchOf('C', [ci('c5', 1_700_000_096), ci('c4', 1_700_000_095)], { color: 2, forkHash: 'F2', forkTime: 1_700_000_080 }),
  D: branchOf('D', [ci('d2', 1_700_000_086), ci('d1', 1_700_000_084)], { color: 3, merged: true }),
};

export function layoutView(section: Section = layoutSection): View {
  return {
    repo: '/w/repo',
    settings: { trunks: [], hidePrefix: '', oldDays: 30, manualTrunk: {}, createdBase: {} },
    groups: [],
    unknown: [],
    old: [],
    branches: layoutBranches,
    sections: [section],
    fetchedAt: 0,
    fetchError: '',
  };
}

// TestLayoutForkNewerThanOwn, transcribed: the fork of A (80) is newer than its
// own commit (70), so the fork row sorts above the commit.
export const forkNewerSection: Section = {
  trunk: 'main',
  lanes: 3,
  rows: [
    row('head', 0, '', 'H', 100),
    row('fork', 0, '', 'F1', 80, 0, [1]),
    row('commit', 2, 'B', 'b1', 75),
    row('commit', 1, 'A', 'a1', 70),
    row('fork', 0, '', 'F2', 60, 0, [2]),
  ],
};
