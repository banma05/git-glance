import { HostError } from './types';
import type { Host, ThemeKind } from './types';
import type { BranchView, Global, Group, Row, Section, Shelf, SwitchResult, TrunkView, View } from '../types';

// Invented, generic data for developing the UI without a core: ?mock=1.
// Add &mode=sidebar for the narrow layout, &theme=light for the light theme and
// &lang=zh-CN for Chinese, &repos=0 for a start without any repository and
// &lanes=many for a third trunk with eight branches with long names side by side.

const DAY = 86400;
// A deterministic pseudo hash: 40 hex digits derived from n.
const hash = (n: number) => {
  let x = (n + 1) * 2654435761;
  let out = '';
  for (let i = 0; i < 5; i++) {
    x = Math.imul(x ^ (x >>> 15), 2246822519) ^ Math.imul(x ^ (x >>> 13), 3266489917);
    x = (x ^ (x >>> 16)) >>> 0;
    out += x.toString(16).padStart(8, '0');
  }
  return out;
};

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
    error: '',
    ...over,
    // every folder that is there has a HEAD
    ...(over.folder && !over.folderMissing && over.folderHead === undefined ? { folderHead: hash(800 + over.color!) } : {}),
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
  // the author of commit n: the person whose branch it is on (n / 10 picks the branch)
  const AUTHORS = ['', 'alice', 'bob', 'carol', 'alice', 'dave', 'erin', 'erin'];
  const c = (n: number, subject: string, ago: number) => ({ hash: hash(n), subject, time: now - ago * DAY, author: AUTHORS[Math.floor(n / 10)] ?? 'alice' });
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
      trunk: trunk('main', { time: now - 5 * DAY, folder: dir, test: 'test', releases: ['release/1.0'] }),
      branches: ['alice/feature/login', 'bob/fix/timeout', 'carol/feature/search', 'alice/fix/typo'],
    },
    {
      trunk: trunk('develop', { hash: hash(102), time: now - 2 * DAY, localBehind: 3 }),
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
        // release/0.9 was deleted after its release: the view no longer has it
        { name: 'main', test: 'test', releases: ['release/1.0', 'release/0.9'] },
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

// withManyLanes adds a trunk "v2" whose eight branches, with long names, are all
// open at once: eight lanes next to each other, as on a busy repository.
export function withManyLanes(view: View): View {
  const now = Math.floor(Date.now() / 1000);
  const names = [
    'alice/platform-migration/checkout-summary-redesign',
    'bob/feature/login-page-redesign',
    'carol/fix/session-timeout-on-mobile',
    'dave/chore/upgrade-build-tooling',
    'erin/feature/export-csv-reports',
    'frank/fix/typo-in-footer',
    'grace/feature/search-filters-and-sort',
    'heidi/spike/cache-warmup',
  ];
  const branches = { ...view.branches };
  const rows: Row[] = [{ kind: 'head', lane: 0, branch: '', hash: hash(500), time: now - DAY, count: 0, ends: [] }];
  names.forEach((name, i) => {
    branches[name] = branch(name, {
      color: i,
      trunk: 'v2',
      folder: i % 3 === 0 ? `/home/dev/demo-${i}` : '',
      folderHead: i % 3 === 0 ? hash(600 + i) : '',
      dirty: i === 0 ? { changed: 3, untracked: 0, files: ['a.ts', 'b.ts', 'c.ts'] } : null,
      behind: i === 1 ? 2 : 0,
      own: [{ hash: hash(510 + i), subject: `Change number ${i + 1}`, time: now - (i + 2) * DAY, author: name.split('/')[0] }],
      forkHash: hash(520),
      lastCommit: now - (i + 2) * DAY,
    });
    rows.push({ kind: 'commit', lane: i + 1, branch: name, hash: hash(510 + i), time: now - (i + 2) * DAY, count: 0, ends: [] });
  });
  rows.push({ kind: 'more', lane: 0, branch: '', hash: '', time: now - 11 * DAY, count: 40, ends: [] });
  rows.push({ kind: 'fork', lane: 0, branch: '', hash: hash(520), time: now - 12 * DAY, count: 0, ends: names.map((_, i) => i + 1) });
  return {
    ...view,
    settings: { ...view.settings, trunks: [...view.settings.trunks, { name: 'v2', test: '', releases: [] }] },
    groups: [...view.groups, { trunk: trunk('v2', { hash: hash(500), time: now - DAY }), branches: names }],
    branches,
    sections: [...view.sections, { trunk: 'v2', lanes: names.length + 1, rows }],
  };
}

// trunkLog is the newest commits on the trunk, newest first.
function trunkLog() {
  const now = Math.floor(Date.now() / 1000);
  const entries: Array<[string, string]> = [
    ['Merge feature/search into main', 'carol'],
    ['Fix login redirect after a timeout', 'bob'],
    ['Add footer links', 'dave'],
    ['Update build tooling', 'erin'],
    ['Rename the session helper', 'alice'],
    ['Tidy the settings page', 'frank'],
    ['Add a changelog', 'grace'],
    ['Speed up the search index', 'carol'],
  ];
  return entries.map(([subject, author], i) => ({ hash: hash(950 + i), subject, time: now - (i * 7 + 3) * 3600, author }));
}

const NEWS_FILES = [
  { path: 'src/search.ts', status: 'A' },
  { path: 'src/login.ts', status: 'M' },
  { path: 'docs/old notes.md', status: 'D' },
  { path: 'build/config.json', status: 'M' },
];

export function mockHost(): Host {
  const q = new URLSearchParams(location.search);
  const listeners: Array<(t: ThemeKind) => void> = [];
  const repo = '/home/dev/demo';
  const global: Global = {
    repos: q.get('repos') === '0' ? [] : [repo, '/home/dev/other'],
    editor: 'code',
    theme: q.get('theme') === 'light' ? 'light' : 'dark',
  };
  const theme = (): ThemeKind => (global.theme === 'light' ? 'light' : 'dark');
  const delay = <T,>(v: T) => new Promise<T>((r) => setTimeout(() => r(v), 150));
  const nowSec = () => Math.floor(Date.now() / 1000);
  let backupBytes = 4 * 1024 * 1024 + 300 * 1024;
  // The main folder: the branch it is on, its uncommitted files, the branch it
  // was switched away from and the work shelved on the way.
  const main = { on: 'main', dirty: ['src/app.ts', 'notes/todo.md'], previous: '', shelves: [] as Shelf[] };
  const switchMain = (branch: string, shelve: boolean, latest: boolean): SwitchResult => {
    const answer = (over: Partial<SwitchResult>): SwitchResult => ({
      ok: true, reason: '', files: [], path: repo, detail: '', logError: '',
      shelved: null, restored: null, keptShelf: null, fastForwarded: false, aheadNote: '', ...over,
    });
    if (main.dirty.length > 0 && (!shelve || branch === main.on)) return answer({ ok: false, reason: 'dirty', files: [...main.dirty] });
    const shelved: Shelf | null = main.dirty.length
      ? { folder: repo, branch: main.on, hash: hash(990), base: hash(100), time: nowSec(), files: [...main.dirty], why: '' }
      : null;
    const restored = main.shelves.find((x) => x.branch === branch) ?? null;
    main.shelves = [...(shelved ? [shelved] : []), ...main.shelves.filter((x) => x !== restored)];
    main.previous = main.on;
    main.on = branch;
    main.dirty = [];
    return answer({ shelved, restored, fastForwarded: latest && !restored });
  };

  const call = async <T,>(method: string, params?: object): Promise<T> => {
    const p = (params ?? {}) as Record<string, unknown>;
    switch (method) {
      case 'version':
        return { core: 'mock', git: '2.45', gitOK: true } as T;
      case 'repos':
        return (await delay(global.repos)) as T;
      case 'view': {
        const view = mockView(String(p.repo ?? repo));
        return (await delay(q.get('lanes') === 'many' ? withManyLanes(view) : view)) as T;
      }
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
      case 'ops': {
        const entry = (ago: number, op: string, branch: string, folder: string, result: string, detail = '') => ({
          time: nowSec() - ago,
          repo,
          op,
          branch,
          folder,
          command: [],
          before: '',
          after: '',
          result,
          detail,
        });
        return (await delay([
          entry(3600, 'align', 'bob/fix/timeout', '', 'ok'),
          entry(7200, 'remove-folder', 'alice/fix/typo', repo + '-typo', 'ok', 'backed up 1 ignored file'),
          entry(86400, 'create', 'alice/feature/login', repo + '-login', 'ok'),
          entry(90000, 'switch', 'carol/feature/search', repo, 'dirty'),
        ])) as T;
      }
      case 'backups':
        return (await delay({ dir: '/home/dev/.config/git-glance/backups', bytes: backupBytes })) as T;
      case 'clearBackups':
        backupBytes = 0;
        return (await delay({})) as T;
      case 'defaultFolder':
        return (await delay({ path: repo + '-' + String(p.branch ?? '').replace(/\//g, '-') })) as T;
      case 'remoteBranches': {
        const query = String(p.query ?? '').toLowerCase();
        return (await delay(['release/2.0', 'hotfix/login-crash', 'origin-only/spike'].filter((n) => n.includes(query)))) as T;
      }
      case 'compare':
        return (await delay({
          onlyA: [{ hash: hash(11), subject: 'Add login form', time: nowSec() - DAY }],
          onlyB: [{ hash: hash(21), subject: 'Raise request timeout', time: nowSec() - 4 * DAY }],
          files: [
            { path: 'src/login.ts', status: 'A' },
            { path: 'src/config.ts', status: 'M' },
          ],
          filesTruncated: false,
        })) as T;
      case 'fileDiff':
        return (await delay({
          diff: ['@@ -1,2 +1,2 @@', ' const timeout = 30;', '-const retries = 1;', '+const retries = 3;'].join('\n'),
          truncated: false,
        })) as T;
      case 'createFolder':
        return (await delay({ ok: true, reason: '', files: [], path: String(p.path ?? ''), detail: '', logError: '' })) as T;
      case 'removeFolder':
        return (await delay({
          ok: true,
          reason: '',
          files: [],
          backupDir: '/home/dev/.config/git-glance/backups/demo-typo',
          backupBytes: 1536,
          deletedBuild: ['node_modules'],
          skippedLinks: [],
          removedLinks: [],
          detail: '',
          logError: '',
        })) as T;
      case 'discover':
        return (await delay({ repo: String(p.path ?? '') })) as T;
      case 'setRole':
      case 'setRepoSettings':
      case 'assign':
        return (await delay(mockView(String(p.repo ?? repo)).settings)) as T;
      case 'switch':
        return (await delay(switchMain(String(p.branch ?? ''), p.shelve === true, p.latest === true))) as T;
      case 'switchState':
        return (await delay({ previous: main.previous, shelves: main.shelves })) as T;
      case 'trunkNews': {
        // as many trunk commits as the view says the branch is behind
        const name = String(p.branch ?? '').replace(/^refs\/heads\//, '');
        const behind = mockView(repo).branches[name]?.behindTrunk ?? 0;
        return (await delay({
          commits: trunkLog().slice(0, behind),
          total: behind,
          base: hash(202),
          tip: hash(100),
          files: behind === 0 ? [] : NEWS_FILES,
          filesTruncated: false,
        })) as T;
      }
      case 'recentCommits':
        return (await delay(trunkLog().slice(0, Number(p.limit) || 50))) as T;
      case 'commitFiles':
        return (await delay({ parent: hash(999), files: NEWS_FILES.slice(1), truncated: false })) as T;
      case 'fileAt':
        return (await delay({ text: ['# Demo', '', 'A first line.', ''].join('\n'), binary: false, truncated: false })) as T;
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
