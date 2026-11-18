import { HostError } from './types';
import type { Host, ThemeKind } from './types';
import type { BranchView, Global, Group, Row, Section, TrunkView, View } from '../types';

// Invented, generic data for developing the UI without a core: ?mock=1.
// Add &mode=sidebar for the narrow layout, &theme=light for the light theme and
// &lang=zh-CN for Chinese.

const DAY = 86400;
const hash = (n: number) => n.toString(16).padStart(40, '0');

function branch(name: string, over: Partial<BranchView>): BranchView {
  const parts = name.split('/');
  return {
    name,
    short: parts.slice(1).join('/') || name,
    topic: parts[parts.length - 1],
    color: 0,
    trunk: 'main',
    trunkSource: 'count',
    folder: '',
    mainFolder: false,
    folderMissing: false,
    dirty: null,
    hasUpstream: true,
    upstreamGone: false,
    ahead: 0,
    behind: 0,
    unpushed: 0,
    behindTrunk: 0,
    own: [],
    forkHash: hash(1),
    forkTime: 0,
    merged: false,
    test: null,
    release: null,
    canRemoveFolder: false,
    removeBlock: 'not-in-folder',
    old: false,
    lastCommit: 0,
    ...over,
  };
}

function trunk(name: string, over: Partial<TrunkView>): TrunkView {
  return {
    name,
    ref: 'origin/' + name,
    hash: hash(100),
    time: 0,
    hasLocal: true,
    localAhead: 0,
    localBehind: 0,
    folder: '',
    test: '',
    releases: [],
    ...over,
  };
}

export function mockView(repo: string): View {
  const now = Math.floor(Date.now() / 1000);
  const c = (n: number, subject: string, ago: number) => ({ hash: hash(n), subject, time: now - ago * DAY });
  const dir = repo.replace(/[\\/]+$/, '');

  const branches: Record<string, BranchView> = {};
  const add = (b: BranchView) => {
    branches[b.name] = b;
  };
  // One dirty folder, never pushed, partly on test.
  add(
    branch('alice/feature/login', {
      color: 0,
      folder: dir + '-login',
      dirty: { changed: 2, untracked: 1, files: ['src/login.ts', 'src/session.ts', 'notes.txt'] },
      hasUpstream: false,
      unpushed: 3,
      own: [c(11, 'Add login form', 1), c(12, 'Validate password length', 2), c(13, 'Show error banner', 3)],
      forkHash: hash(201),
      forkTime: now - 6 * DAY,
      lastCommit: now - DAY,
      test: {
        state: 'some',
        matched: 2,
        total: 3,
        per: [
          { ...c(11, 'Add login form', 1), by: 'ancestor' },
          { ...c(12, 'Validate password length', 2), by: 'patch' },
          { ...c(13, 'Show error banner', 3), by: '' },
        ],
      },
      release: { state: 'none', in: [], notIn: ['main'] },
      removeBlock: 'dirty',
    }),
  );
  // Behind the remote, fully on test, not released.
  add(
    branch('bob/fix/timeout', {
      color: 1,
      folder: dir + '-timeout',
      behind: 2,
      own: [c(21, 'Raise request timeout', 4)],
      forkHash: hash(202),
      forkTime: now - 8 * DAY,
      behindTrunk: 6,
      lastCommit: now - 4 * DAY,
      test: { state: 'all', matched: 1, total: 1, per: [{ ...c(21, 'Raise request timeout', 4), by: 'ancestor' }] },
      release: { state: 'none', in: [], notIn: ['main'] },
      removeBlock: 'unpushed',
    }),
  );
  // In sync with the remote, no folder.
  add(
    branch('carol/feature/search', {
      color: 2,
      own: [c(31, 'Add search box', 6), c(32, 'Debounce typing', 7)],
      forkHash: hash(203),
      forkTime: now - 10 * DAY,
      lastCommit: now - 6 * DAY,
      test: { state: 'none', matched: 0, total: 2, per: [] },
    }),
  );
  // Merged into the trunk, folder can be removed.
  add(
    branch('alice/fix/typo', {
      color: 3,
      folder: dir + '-typo',
      merged: true,
      canRemoveFolder: true,
      removeBlock: '',
      own: [c(41, 'Fix typo in title', 9)],
      forkTime: now - 12 * DAY,
      lastCommit: now - 9 * DAY,
      test: { state: 'all', matched: 1, total: 1, per: [{ ...c(41, 'Fix typo in title', 9), by: 'ancestor' }] },
      release: { state: 'all', in: ['main'], notIn: [] },
    }),
  );
  // Folder recorded but gone from the disk, on the second trunk.
  add(
    branch('dave/feature/export', {
      color: 4,
      trunk: 'develop',
      folder: dir + '-export',
      folderMissing: true,
      ahead: 1,
      own: [c(51, 'Export to CSV', 3)],
      forkHash: hash(301),
      forkTime: now - 7 * DAY,
      lastCommit: now - 3 * DAY,
      removeBlock: 'folder-missing',
      test: { state: 'all', matched: 1, total: 1, per: [{ ...c(51, 'Export to CSV', 3), by: 'subject' }] },
    }),
  );
  add(
    branch('erin/feature/billing', {
      color: 5,
      trunk: 'develop',
      own: [c(61, 'Add invoice list', 2), c(62, 'Add invoice totals', 2)],
      forkHash: hash(302),
      forkTime: now - 4 * DAY,
      lastCommit: now - 2 * DAY,
      release: { state: 'some', in: ['develop'], notIn: ['release/1.0'] },
    }),
  );
  // Cannot tell which trunk it belongs to.
  add(
    branch('frank/spike/cache', {
      color: 6,
      trunk: '',
      trunkSource: 'unknown',
      hasUpstream: false,
      lastCommit: now - 2 * DAY,
    }),
  );
  // Old branch, not in a folder.
  add(
    branch('erin/chore/cleanup', {
      color: 7,
      trunk: 'develop',
      old: true,
      lastCommit: now - 90 * DAY,
      own: [c(71, 'Remove unused helpers', 90)],
      forkTime: now - 91 * DAY,
    }),
  );

  const groups: Group[] = [
    {
      trunk: trunk('main', { time: now - 5 * DAY, test: 'test', releases: ['release/1.0'] }),
      branches: ['alice/feature/login', 'bob/fix/timeout', 'carol/feature/search', 'alice/fix/typo'],
    },
    {
      trunk: trunk('develop', { time: now - 2 * DAY, localBehind: 3 }),
      branches: ['dave/feature/export', 'erin/feature/billing'],
    },
  ];

  const row = (kind: Row['kind'], lane: number, name: string, n: number, ago: number, count = 0, ends: number[] = []): Row => ({
    kind,
    lane,
    branch: name,
    hash: n ? hash(n) : '',
    time: now - ago * DAY,
    count,
    ends,
  });
  // Rows are newest first, the way the core lays them out.
  const sections: Section[] = [
    {
      trunk: 'main',
      lanes: 4,
      rows: [
        row('commit', 1, 'alice/feature/login', 11, 1),
        row('commit', 1, 'alice/feature/login', 12, 2),
        row('commit', 1, 'alice/feature/login', 13, 3),
        row('commit', 2, 'bob/fix/timeout', 21, 4),
        row('head', 0, '', 100, 5),
        row('commit', 3, 'carol/feature/search', 31, 6),
        row('more', 0, '', 0, 6, 3),
        row('fork', 0, '', 201, 6, 0, [1]),
        row('commit', 3, 'carol/feature/search', 32, 7),
        row('more', 0, '', 0, 8, 2),
        row('fork', 0, '', 202, 8, 0, [2]),
        row('merge', 1, 'alice/fix/typo', 0, 9, 1),
        row('more', 0, '', 0, 10, 12),
        row('fork', 0, '', 203, 10, 0, [3]),
      ],
    },
    {
      trunk: 'develop',
      lanes: 3,
      rows: [
        row('head', 0, '', 102, 2),
        row('commit', 1, 'erin/feature/billing', 61, 2),
        row('commit', 1, 'erin/feature/billing', 62, 2),
        row('commit', 2, 'dave/feature/export', 51, 3),
        row('more', 0, '', 0, 4, 2),
        row('fork', 0, '', 302, 4, 0, [1]),
        row('more', 0, '', 0, 7, 5),
        row('fork', 0, '', 301, 7, 0, [2]),
      ],
    },
  ];

  return {
    repo,
    settings: {
      trunks: [
        { name: 'main', test: 'test', releases: ['release/1.0'] },
        { name: 'develop', test: '', releases: [] },
      ],
      hidePrefix: '',
      oldDays: 30,
      manualTrunk: {},
      createdBase: {},
    },
    groups,
    unknown: ['frank/spike/cache'],
    old: ['erin/chore/cleanup'],
    branches,
    sections,
    fetchedAt: now - 60,
    fetchError: '',
  };
}

export function mockHost(): Host {
  const q = new URLSearchParams(location.search);
  const listeners: Array<(t: ThemeKind) => void> = [];
  const repo = '/home/dev/demo';
  const global: Global = {
    repos: [repo, '/home/dev/other'],
    editor: 'code',
    theme: q.get('theme') === 'light' ? 'light' : 'dark',
  };
  const theme = (): ThemeKind => (global.theme === 'light' ? 'light' : 'dark');
  const delay = <T,>(v: T) => new Promise<T>((r) => setTimeout(() => r(v), 150));
  const nowSec = () => Math.floor(Date.now() / 1000);

  const call = async <T,>(method: string, params?: object): Promise<T> => {
    const p = (params ?? {}) as Record<string, unknown>;
    switch (method) {
      case 'repos':
        return (await delay(global.repos)) as T;
      case 'view':
        return (await delay(mockView(String(p.repo ?? repo)))) as T;
      case 'fetch':
        return (await delay({ ok: true, attempts: 1, error: '', at: nowSec() })) as T;
      case 'getGlobal':
        return { ...global } as T;
      case 'setGlobal': {
        const before = theme();
        if (typeof p.editor === 'string') global.editor = p.editor;
        if (p.theme === 'auto' || p.theme === 'dark' || p.theme === 'light') global.theme = p.theme;
        if (theme() !== before) listeners.forEach((cb) => cb(theme()));
        return { ...global } as T;
      }
      case 'align':
        return (await delay({
          fetch: { ok: true, attempts: 1, error: '', at: nowSec() },
          updated: [{ branch: 'bob/fix/timeout', from: hash(21).slice(0, 7), to: hash(22).slice(0, 7), folder: '' }],
          skipped: [{ branch: 'alice/feature/login', reason: 'dirty', files: ['src/login.ts'], detail: '' }],
          logError: '',
        })) as T;
      case 'ops':
        return [] as T;
      case 'switch':
        return (await delay({ ok: true, reason: '', files: [], path: '', logError: '' })) as T;
      case 'trunkCommits':
        return (await delay([
          { hash: hash(901), subject: 'Update docs', time: nowSec() - 2 * DAY },
          { hash: hash(902), subject: 'Bump a dependency', time: nowSec() - 3 * DAY },
          { hash: hash(903), subject: 'Tidy a helper', time: nowSec() - 4 * DAY },
        ])) as T;
      case 'moveTrunk':
      case 'addRepo':
        return (await delay(global.repos)) as T;
      default:
        throw new HostError('unknown-method', 'the mock host does not know ' + method);
    }
  };

  return {
    call,
    openFolder: async () => undefined,
    copy: async (text) => navigator.clipboard?.writeText(text),
    openDiff: async () => false,
    openWide: () => undefined,
    mode: q.get('mode') === 'sidebar' ? 'sidebar' : 'wide',
    locale: q.get('lang') ?? navigator.language ?? 'en',
    theme,
    onTheme: (cb) => {
      listeners.push(cb);
    },
  };
}
