import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import { rpc } from './api';
import { HostError } from './host/types';
import type { DiscoveryProblem, Host, ThemeKind } from './host/types';
import { baseName } from './reasons';
import type { AlignReport, DirtyCounts, Global, OpResult, RemoveFolderResult, SwitchResult, SwitchState, Theme, View } from './types';

export type ViewMode = 'list' | 'graph';
// create: a new branch from a trunk; createExisting: a folder for the selected local branch.
// shelve: the confirm that lists the uncommitted changes a switch would shelve.
export type DialogName = 'align' | 'create' | 'createExisting' | 'compare' | 'oplog' | 'settings' | 'remove' | 'shelve';

// WriteOutcome is what a create or remove write ended with: the core's answer,
// or the error of the call. It lives in the state so that a dialog closed while
// the write runs still gets to show it.
export interface WriteOutcome<R> {
  branch: string;
  result: R | null;
  error: unknown;
}

export type WriteKind = 'create' | 'remove' | 'setRole' | 'repoSettings' | 'clearBackups' | 'align';

// Notice is a write's result shown on the page when the dialog it was started
// from is no longer open: which write, on what, and how it ended.
export interface Notice {
  kind: WriteKind;
  target: string; // branch or repository; may be ""
  error: unknown; // the failed call; null when the core answered
  ok: boolean;
  reason: string; // the core's refusal reason when !ok and there is no error
  logError: string;
}

// SettingsOutcome is the last settings write, shown inside the settings dialog.
export interface SettingsOutcome {
  kind: WriteKind;
  target: string;
  error: unknown; // null when the write worked
}

// What a click in the graph picked besides a whole branch: one commit of a
// branch, or a commit on a trunk (head or fork point, or one of the hidden ones).
export type TrunkPick = { trunk: string; hash: string; time: number; subject: string };
export type Picked = ({ kind: 'trunk' } & TrunkPick) | { kind: 'commit'; branch: string; hash: string };

// NewsTarget is what the open trunk news list is about: the trunk commits a
// branch lacks, or the recent commits of a trunk. It belongs to the selection.
export type NewsTarget = { kind: 'branch'; branch: string } | { kind: 'trunk'; trunk: string };

// SwitchTarget is where a switch of the main folder goes: a branch at its
// latest (after a read of the remote) or as it is, through a remote when the
// branch exists only there. back: the "switch back" control asked.
export interface SwitchTarget {
  branch: string;
  latest: boolean;
  remote: string;
  back: boolean;
}

// ShelveAsk is a switch waiting for the user to confirm that the listed
// uncommitted changes are shelved first.
export interface ShelveAsk {
  repo: string;
  target: SwitchTarget;
  folder: string;
  files: string[];
  more: number; // changed files not listed
  fetched: boolean; // the remote was read for this switch already
}

// MainFolder is what the view says of the repository's main folder: where it
// is and, when it is on a branch the view lists, that branch and its changes.
export interface MainFolder {
  folder: string;
  branch: string; // "" when the view cannot tell
  dirty: DirtyCounts | null; // null: clean, or not known (the folder is on a trunk)
}

// mainFolderOf finds the main folder: the folder of the branch checked out
// there, else the repository path; and which branch it is on.
export function mainFolderOf(view: View, repo: string): MainFolder {
  const b = Object.values(view.branches).find((x) => x.mainFolder && x.folder);
  if (b) return { folder: b.folder, branch: b.name, dirty: b.dirty };
  const trunk = view.groups.find((g) => g.trunk.folder && samePath(g.trunk.folder, repo));
  return { folder: repo, branch: trunk?.trunk.name ?? '', dirty: null };
}

const samePath = (a: string, b: string) => inside(a, b) && inside(b, a);

// GitProblem is why the core refuses every change: git is older than 2.30
// (version known), missing, or a write was refused with git-too-old.
export type GitProblem = { kind: 'old' | 'missing' | 'refused'; version: string };

export interface AppState {
  repos: string[];
  repo: string;
  view: View | null;
  loaded: boolean; // the repository list (and the first view when there is a repository) arrived, or failed
  listed: boolean; // the repository list has answered at least once (false: it failed or is pending)
  discoveryProblems: DiscoveryProblem[]; // workspace folders the editor could not add, and why
  fetching: boolean;
  fetchError: string; // "" when the last read of the remote worked
  fetchSlow: boolean; // the last read of the remote ran out of time and goes on in the core
  error: unknown; // a failed call other than the remote read; null when none
  noToken: boolean;
  mode: ViewMode;
  selected: string; // branch name, "" when none
  selectedTrunk: string; // trunk whose details are shown, "" when none; never set together with selected or pick
  pick: Picked | null; // a commit or trunk commit picked in the graph
  news: NewsTarget | null; // the trunk news list shown under the details, null when closed
  collapsed: Record<string, boolean>; // group key -> folded
  dialog: DialogName | null;
  alignReport: AlignReport | null;
  busy: boolean; // a write is running
  createOutcome: WriteOutcome<OpResult> | null;
  removeOutcome: WriteOutcome<RemoveFolderResult> | null;
  settingsOutcome: SettingsOutcome | null;
  notice: Notice | null;
  // the last switch: shown in the details of its branch, or with the switch-back control (back)
  switchResult: { branch: string; result: SwitchResult; back?: boolean; inPlace?: boolean } | null;
  switchState: SwitchState | null; // the main folder's previous branch and the shelves; null until read
  ask: ShelveAsk | null; // a switch waiting for the shelving confirm
  assignResult: { branch: string; error: unknown } | null; // the last failed "belongs to trunk", shown in the detail area
  global: Global | null;
  theme: ThemeKind;
  gitProblem: GitProblem | null; // set: every button that changes something is disabled
}

export interface Actions {
  selectRepo(repo: string): void;
  addRepo(path: string): void;
  reload(): void; // open the current repository again (after a failed first view)
  reloadRepos(): void; // read the repository list again (after it failed)
  refresh(): void; // read the remote again, then the view (the retry button)
  setMode(mode: ViewMode): void;
  select(branch: string): void;
  selectCommit(branch: string, hash: string): void;
  selectTrunk(p: TrunkPick): void;
  showTrunk(name: string): void; // the details of a trunk itself; again hides them
  toggleNews(target: NewsTarget): void; // open the trunk news list; again closes it
  // switch the main folder to a branch, by default at its latest (the remote is read first)
  switchTo(branch: string, opt?: { latest?: boolean; remote?: string }): void;
  switchBack(): void; // switch the main folder back to the branch it was switched away from
  confirmShelve(): void; // the user confirmed the files a waiting switch shelves
  openFolder(path: string): void;
  copy(text: string): void;
  toggleGroup(key: string): void;
  openDialog(name: DialogName): void;
  closeDialog(): void;
  refreshView(): void; // read the view again (after a change made outside the actions)
  // repo is the repository the dialog was opened for; nothing is sent when the page has moved on.
  // existing: a folder for a local branch; trunk is unused.
  createFolder(repo: string, trunk: string, branch: string, path: string, existing?: boolean): void;
  assign(branch: string, trunk: string): void; // trunk "" goes back to deciding by the commits
  removeFolder(repo: string, branch: string, folder: string, head: string): void; // as the confirm dialog showed them
  setRole(branch: string, role: string, trunk: string): void;
  setRepoSettings(hidePrefix: string, oldDays: number): void;
  clearBackups(): void;
  dismissNotice(): void;
  align(): void;
  moveTrunk(name: string, index: number): void;
  cycleTheme(): void;
  setEditor(editor: string): void;
  setTheme(theme: Theme): void;
  openWide(): void;
}

const THEME_ORDER: Theme[] = ['auto', 'dark', 'light'];

// The group keys the list folds: "trunk:<name>", "unknown" and "old".
export const trunkKey = (name: string) => `trunk:${name}`;

// inside reports whether folder is root or a folder in it. Separators and the
// case are ignored: a drive letter or a path may come from the editor in
// another spelling than from the core.
export function inside(folder: string, root: string): boolean {
  const norm = (p: string) => p.replace(/\\/g, '/').replace(/\/+$/, '').toLowerCase();
  const f = norm(folder);
  const r = norm(root);
  return f === r || f.startsWith(r + '/');
}

function initial(host: Host): AppState {
  return {
    repos: [],
    repo: '',
    view: null,
    loaded: false,
    listed: false,
    discoveryProblems: [],
    fetching: false,
    fetchError: '',
    fetchSlow: false,
    error: null,
    noToken: false,
    mode: 'list',
    selected: '',
    selectedTrunk: '',
    pick: null,
    news: null,
    collapsed: {},
    dialog: null,
    alignReport: null,
    busy: false,
    createOutcome: null,
    removeOutcome: null,
    settingsOutcome: null,
    notice: null,
    switchResult: null,
    switchState: null,
    ask: null,
    assignResult: null,
    global: null,
    theme: host.theme(),
    gitProblem: null,
  };
}

// useApp holds the state of the whole screen and the actions on it. Every call
// goes through the host; nothing repeats on its own: the remote is read when a
// repository opens and when the user asks again.
export function useApp(host: Host): { state: AppState; actions: Actions } {
  const [state, setState] = useState<AppState>(() => initial(host));
  const ref = useRef(state);

  const { actions, start, reread, discovered } = useMemo(() => {
    // patch updates the latest state at once, so the next step of a running
    // action reads what the previous step wrote.
    const patch = (p: Partial<AppState>) => {
      ref.current = { ...ref.current, ...p };
      setState(ref.current);
    };
    // noteGit turns a git-too-old refusal into the git line; true when it was one.
    const noteGit = (e: unknown) => {
      if (!(e instanceof HostError) || e.code !== 'git-too-old') return false;
      if (!ref.current.gitProblem) patch({ gitProblem: { kind: 'refused', version: '' } });
      return true;
    };
    const fail = (e: unknown) => {
      if (e instanceof HostError && e.code === 'cancelled') return; // the host dropped it on purpose
      if (noteGit(e)) patch({ loaded: true });
      else if (e instanceof HostError && e.code === 'no-token') patch({ noToken: true, loaded: true });
      else patch({ error: e, loaded: true });
    };
    // canWrite is false while a write runs or when the core refuses every change.
    const canWrite = () => !ref.current.busy && !ref.current.gitProblem;
    // gitRan: the reasons of a result after which git did run, so the view changed.
    const gitRan = (r: OpResult) => r.ok || r.reason === 'git-failed' || r.reason === 'verify-failed';
    // switchRan: git changed something (a switch, a shelf taken or put back)
    const switchRan = (r: SwitchResult) => gitRan(r) || !!r.shelved || !!r.restored || !!r.keptShelf || r.reason === 'restore-failed';
    const current = (repo: string) => ref.current.repo === repo;

    // failFor reports a failure of a call made for `repo`; once the user has moved
    // to another repository the failure is not theirs to see.
    const failFor = (repo: string, e: unknown) => {
      if (current(repo)) fail(e);
    };
    // noteIfClosed puts a write's result on the page when its dialog was closed
    // meanwhile; while the dialog is open it shows the result itself.
    const noteIfClosed = (dialog: DialogName, notice: Notice) => {
      if (ref.current.dialog !== dialog) patch({ notice });
    };
    const reasonOf = (e: unknown) => (e instanceof Error && e.message ? e.message : String(e));

    // newsOf is the open news list while it is about this selection, else null.
    const newsOf = (selected: string, selectedTrunk: string): NewsTarget | null => {
      const n = ref.current.news;
      if (n?.kind === 'branch' && n.branch === selected) return n;
      if (n?.kind === 'trunk' && n.trunk === selectedTrunk) return n;
      return null;
    };

    // stale is what of the selection a new view no longer has: a branch that is
    // gone, a commit no longer among its own, a trunk that is gone.
    const stale = (view: View): Partial<AppState> => {
      const { selected, selectedTrunk, pick } = ref.current;
      const out: Partial<AppState> = {};
      if (selected && !view.branches[selected]) Object.assign(out, { selected: '', switchResult: null, assignResult: null });
      if (pick?.kind === 'commit' && !view.branches[pick.branch]?.own.some((c) => c.hash === pick.hash)) out.pick = null;
      if (pick?.kind === 'trunk' && !view.sections.some((s) => s.trunk === pick.trunk)) out.pick = null;
      if (selectedTrunk && !view.groups.some((g) => g.trunk.name === selectedTrunk)) out.selectedTrunk = '';
      if (ref.current.news && !newsOf(out.selected ?? selected, out.selectedTrunk ?? selectedTrunk)) out.news = null;
      return out;
    };

    const loadView = async (repo: string) => {
      const view = await rpc(host, 'view', { repo });
      if (current(repo)) patch({ view, fetchError: view.fetchError, error: null, ...stale(view) });
      return view;
    };

    // refresh reads the remote in the background, then the view again. A failed
    // call lands in the "could not read the remote" line, which has the retry.
    const refresh = async (repo: string) => {
      // the core counts fetch as a change, so with too old a git there is none
      if (ref.current.gitProblem) return;
      patch({ fetching: true, fetchSlow: false });
      try {
        const f = await rpc(host, 'fetch', { repo });
        const view = await rpc(host, 'view', { repo });
        if (!current(repo)) return;
        const failed = f.ok ? view.fetchError : f.error || view.fetchError || 'unknown';
        patch({ view, fetching: false, fetchError: failed, error: null, ...stale(view) });
      } catch (e) {
        if (!current(repo)) return;
        if (noteGit(e) || (e instanceof HostError && e.code === 'cancelled')) {
          // the git line says why, or the host dropped it on purpose
        } else if (e instanceof HostError && e.code === 'still-running') patch({ fetchSlow: true });
        else if (e instanceof HostError && e.code === 'no-token') fail(e);
        else patch({ fetchError: reasonOf(e) });
        patch({ fetching: false });
      }
    };

    // readSwitchState reads where the main folder was switched away from and
    // the shelves; a failed or odd answer leaves what the page showed.
    const readSwitchState = async (repo: string) => {
      const view = ref.current.view;
      if (!view) return;
      try {
        const s = await rpc(host, 'switchState', { repo, folder: mainFolderOf(view, repo).folder });
        if (current(repo) && typeof s?.previous === 'string' && Array.isArray(s.shelves)) patch({ switchState: s });
      } catch {
        // the switch-back line stays as it was
      }
    };

    // readRemote reads the remote before a switch to the latest. A failure is
    // shown in the fetch line; the switch then goes to what was read before.
    const readRemote = async (repo: string) => {
      try {
        const f = await rpc(host, 'fetch', { repo });
        if (current(repo)) patch({ fetchError: f.ok ? '' : f.error || 'unknown', fetchSlow: false });
      } catch (e) {
        if (!current(repo) || noteGit(e) || (e instanceof HostError && e.code === 'cancelled')) return;
        if (e instanceof HostError && e.code === 'still-running') patch({ fetchSlow: true });
        else if (e instanceof HostError && e.code === 'no-token') throw e;
        else patch({ fetchError: reasonOf(e) });
      }
    };

    // switchMain switches the main folder. Without the user's confirm nothing
    // is shelved: the first try never shelves, and when the folder has changes
    // the confirm lists them; only its button sends the switch that shelves.
    const switchMain = async (target: SwitchTarget, shelve: boolean, fetched: boolean) => {
      const { repo, view } = ref.current;
      if (!repo || !view || !canWrite()) return;
      const main = mainFolderOf(view, repo);
      const away = main.branch !== target.branch;
      if (!shelve && away && main.dirty) {
        // the view knows there are changes: ask before anything is sent
        const files = main.dirty.files;
        const more = Math.max(0, main.dirty.changed + main.dirty.untracked - files.length);
        patch({ ask: { repo, target, folder: main.folder, files, more, fetched }, dialog: 'shelve', switchResult: null });
        return;
      }
      patch({ busy: true, switchResult: null, ask: null });
      try {
        if (target.latest && !fetched) {
          await readRemote(repo);
          fetched = true;
        }
        if (!current(repo)) return;
        const params = { repo, folder: main.folder, branch: target.branch, latest: target.latest, shelve, remote: target.remote };
        const result = await rpc(host, 'switch', params);
        if (!current(repo)) return;
        // a dirty folder that is already on the branch cannot be updated under shelved work
        const inPlace = !result.ok && result.reason === 'dirty' && (shelve || !away);
        if (!result.ok && result.reason === 'dirty' && !inPlace) {
          patch({ ask: { repo, target, folder: main.folder, files: result.files, more: 0, fetched }, dialog: 'shelve' });
        } else {
          patch({ switchResult: { branch: target.branch, result, back: target.back, inPlace } });
        }
        if (fetched || switchRan(result)) await loadView(repo);
        if (switchRan(result)) await readSwitchState(repo);
      } catch (e) {
        failFor(repo, e);
      } finally {
        patch({ busy: false });
      }
    };

    const open = async (repo: string) => {
      // fetching is reset here: a refresh of the repository left behind drops its result
      patch({ repo, view: null, selected: '', selectedTrunk: '', pick: null, news: null, switchState: null, ask: null, fetchError: '', fetchSlow: false, error: null, alignReport: null, switchResult: null, createOutcome: null, removeOutcome: null, fetching: false });
      try {
        await loadView(repo);
        if (!current(repo)) return;
        patch({ loaded: true });
      } catch (e) {
        failFor(repo, e);
        return;
      }
      void readSwitchState(repo);
      await refresh(repo);
      if (current(repo) && ref.current.global === null) {
        try {
          patch({ global: await rpc(host, 'getGlobal') });
        } catch {
          // the editor and theme choices stay at their defaults
        }
      }
    };

    const writeGlobal = async (params: { editor?: string; theme?: Theme }) => {
      if (ref.current.gitProblem) return;
      try {
        patch({ global: await rpc(host, 'setGlobal', params), theme: host.theme() });
      } catch (e) {
        fail(e);
      }
    };

    // checkGit asks the core which git it found; an old or missing git disables
    // every change before the user tries one.
    const checkGit = async () => {
      try {
        const v = await rpc(host, 'version');
        if (v?.gitOK === false) patch({ gitProblem: v.git ? { kind: 'old', version: v.git } : { kind: 'missing', version: '' } });
      } catch {
        // the writes report git-too-old themselves
      }
    };

    // readList reads the repository list. Only the latest read has the say:
    // an older one that ends later gives null, so a late answer never undoes
    // what a newer one opened.
    let lists = 0;
    const readList = async (): Promise<string[] | null> => {
      const mine = ++lists;
      try {
        const repos = await rpc(host, 'repos');
        if (mine !== lists) return null;
        patch({ repos, listed: true, error: null });
        return repos;
      } catch (e) {
        if (mine === lists) throw e;
        return null;
      }
    };

    const start = async () => {
      void checkGit();
      try {
        const repos = await readList();
        if (!repos) return;
        if (repos.length === 0) patch({ loaded: true });
        else await open(repos[0]);
      } catch (e) {
        fail(e);
      }
    };

    // reread answers the host's "refresh" and the Retry of a failed list: the
    // repository list is read again; the open repository keeps its place and
    // only its view is read again, and with none open the first one opens.
    const reread = async () => {
      try {
        const repos = await readList();
        if (!repos) return;
        const cur = ref.current.repo;
        if (cur && repos.includes(cur)) {
          await loadView(cur).catch((e) => failFor(cur, e));
        } else if (repos.length > 0) {
          await open(repos[0]);
        } else {
          patch({ repo: '', view: null, selected: '', selectedTrunk: '', pick: null, news: null, loaded: true });
        }
      } catch (e) {
        fail(e);
      }
    };
    // discovered takes what the editor could not add to the list. Too old a git
    // refuses addRepo, and the git line says that already.
    const discovered = (problems: DiscoveryProblem[]) => {
      if (problems.some((p) => p.code === 'git-too-old') && !ref.current.gitProblem) {
        patch({ gitProblem: { kind: 'refused', version: '' } });
      }
      patch({ discoveryProblems: problems.filter((p) => p.code !== 'git-too-old') });
    };

    const actions: Actions = {
      selectRepo: (repo) => {
        if (repo !== ref.current.repo) void open(repo);
      },
      addRepo: (path) => {
        if (ref.current.gitProblem) return;
        void (async () => {
          try {
            const repos = await rpc(host, 'addRepo', { path });
            patch({ repos });
            const added = repos.find((r) => r.replace(/[\\/]+$/, '') === path.replace(/[\\/]+$/, '')) ?? repos[repos.length - 1];
            // an alert about a folder in the added repository is answered now
            if (added) patch({ discoveryProblems: ref.current.discoveryProblems.filter((p) => !p.folder || !inside(p.folder, added)) });
            if (added) await open(added);
          } catch (e) {
            fail(e);
          }
        })();
      },
      reload: () => {
        if (ref.current.repo) void open(ref.current.repo);
      },
      reloadRepos: () => void reread(),
      refresh: () => {
        if (ref.current.repo && !ref.current.fetching) void refresh(ref.current.repo);
      },
      // a picked commit belongs to the graph it was clicked in; the branch or trunk stays selected
      setMode: (mode) => patch(mode === ref.current.mode ? {} : { mode, pick: null }),
      select: (branch) => {
        const selected = ref.current.selected === branch && !ref.current.pick ? '' : branch;
        patch({ selected, selectedTrunk: '', pick: null, news: newsOf(selected, ''), switchResult: null, assignResult: null });
      },
      selectCommit: (branch, hash) => {
        const p = ref.current.pick;
        const same = p?.kind === 'commit' && p.branch === branch && p.hash === hash;
        patch(
          same
            ? { selected: '', pick: null, news: null, switchResult: null }
            : { selected: branch, selectedTrunk: '', pick: { kind: 'commit', branch, hash }, news: newsOf(branch, ''), switchResult: null },
        );
      },
      selectTrunk: (p) => {
        const cur = ref.current.pick;
        const same = cur?.kind === 'trunk' && cur.hash === p.hash;
        patch(
          same
            ? { selected: '', pick: null, news: null }
            : { selected: '', selectedTrunk: '', pick: { kind: 'trunk', ...p }, news: null, switchResult: null },
        );
      },
      showTrunk: (name) => {
        const selectedTrunk = ref.current.selectedTrunk === name ? '' : name;
        patch({ selectedTrunk, selected: '', pick: null, news: newsOf('', selectedTrunk), switchResult: null, assignResult: null });
      },
      toggleNews: (target) => {
        const n = ref.current.news;
        const same = n?.kind === target.kind && (n.kind === 'branch' ? n.branch : n.trunk) === (target.kind === 'branch' ? target.branch : target.trunk);
        patch({ news: same ? null : target });
      },
      switchTo: (branch, opt = {}) => void switchMain({ branch, latest: opt.latest ?? true, remote: opt.remote ?? '', back: false }, false, false),
      switchBack: () => {
        const previous = ref.current.switchState?.previous;
        if (previous) void switchMain({ branch: previous, latest: false, remote: '', back: true }, false, false);
      },
      confirmShelve: () => {
        const a = ref.current.ask;
        patch({ ask: null, dialog: null });
        if (a && current(a.repo)) void switchMain(a.target, true, a.fetched);
      },
      openFolder: (path) => {
        host.openFolder(path).catch(fail);
      },
      copy: (text) => {
        host.copy(text).catch(fail);
      },
      toggleGroup: (key) => patch({ collapsed: { ...ref.current.collapsed, [key]: !ref.current.collapsed[key] } }),
      openDialog: (dialog) => {
        // a write that is still running keeps its slot; otherwise the dialog starts clean
        patch(ref.current.busy ? { dialog } : { dialog, createOutcome: null, removeOutcome: null, settingsOutcome: null });
      },
      refreshView: () => {
        const repo = ref.current.repo;
        if (repo) loadView(repo).catch((e) => failFor(repo, e));
      },
      dismissNotice: () => patch({ notice: null }),
      setRole: (branch, role, trunk) => {
        const repo = ref.current.repo;
        if (!repo || !canWrite()) return;
        patch({ busy: true, settingsOutcome: null });
        void (async () => {
          let error: unknown = null;
          try {
            const params = role === 'test' || role === 'release' ? { repo, branch, role, trunk } : { repo, branch, role };
            await rpc(host, 'setRole', params);
          } catch (e) {
            error = e;
            noteGit(e);
          }
          patch({ settingsOutcome: { kind: 'setRole', target: branch, error } });
          noteIfClosed('settings', { kind: 'setRole', target: branch, error, ok: !error, reason: '', logError: '' });
          try {
            if (!error) await loadView(repo);
          } catch (e) {
            failFor(repo, e);
          } finally {
            patch({ busy: false });
          }
        })();
      },
      setRepoSettings: (hidePrefix, oldDays) => {
        const repo = ref.current.repo;
        if (!repo || !canWrite()) return;
        patch({ busy: true, settingsOutcome: null });
        void (async () => {
          let error: unknown = null;
          try {
            await rpc(host, 'setRepoSettings', { repo, hidePrefix, oldDays });
          } catch (e) {
            error = e;
            noteGit(e);
          }
          patch({ settingsOutcome: { kind: 'repoSettings', target: repo, error } });
          noteIfClosed('settings', { kind: 'repoSettings', target: repo, error, ok: !error, reason: '', logError: '' });
          try {
            if (!error) await loadView(repo);
          } catch (e) {
            failFor(repo, e);
          } finally {
            patch({ busy: false });
          }
        })();
      },
      clearBackups: () => {
        if (!canWrite()) return;
        patch({ busy: true, settingsOutcome: null });
        void (async () => {
          let error: unknown = null;
          try {
            await rpc(host, 'clearBackups');
          } catch (e) {
            error = e;
            noteGit(e);
          }
          patch({ settingsOutcome: { kind: 'clearBackups', target: '', error } });
          noteIfClosed('settings', { kind: 'clearBackups', target: '', error, ok: !error, reason: '', logError: '' });
          patch({ busy: false });
        })();
      },
      createFolder: (repo, trunk, branch, path, existing = false) => {
        if (!repo || repo !== ref.current.repo || !canWrite()) return;
        patch({ busy: true, createOutcome: null });
        void (async () => {
          try {
            let result: OpResult;
            try {
              const params = existing ? { repo, branch, path, existing: true } : { repo, trunk, branch, path };
              result = await rpc(host, 'createFolder', params);
            } catch (error) {
              noteGit(error);
              if (current(repo)) patch({ createOutcome: { branch, result: null, error } });
              noteIfClosed('create', { kind: 'create', target: branch, error, ok: false, reason: '', logError: '' });
              return;
            }
            if (current(repo)) patch({ createOutcome: { branch, result, error: null } });
            noteIfClosed('create', { kind: 'create', target: branch, error: null, ok: result.ok, reason: result.reason, logError: result.logError });
            if (gitRan(result)) await loadView(repo);
          } catch (e) {
            failFor(repo, e);
          } finally {
            patch({ busy: false });
          }
        })();
      },
      removeFolder: (repo, branch, folder, head) => {
        if (!repo || repo !== ref.current.repo || !canWrite()) return;
        patch({ busy: true, removeOutcome: null });
        void (async () => {
          try {
            let result: RemoveFolderResult;
            try {
              result = await rpc(host, 'removeFolder', { repo, branch, folder, head });
            } catch (error) {
              noteGit(error);
              if (current(repo)) patch({ removeOutcome: { branch, result: null, error } });
              noteIfClosed('remove', { kind: 'remove', target: branch, error, ok: false, reason: '', logError: '' });
              return;
            }
            if (current(repo)) patch({ removeOutcome: { branch, result, error: null } });
            noteIfClosed('remove', { kind: 'remove', target: branch, error: null, ok: result.ok, reason: result.reason, logError: result.logError });
            // the folder may be gone even when the answer is a refusal after a partial run
            await loadView(repo);
          } catch (e) {
            failFor(repo, e);
          } finally {
            patch({ busy: false });
          }
        })();
      },
      assign: (branch, trunk) => {
        const repo = ref.current.repo;
        if (!repo || !canWrite()) return;
        patch({ busy: true, assignResult: null });
        void (async () => {
          let error: unknown = null;
          try {
            await rpc(host, 'assign', { repo, branch, trunk });
          } catch (e) {
            error = e;
            if (!noteGit(e) && current(repo)) patch({ assignResult: { branch, error: e } });
          }
          try {
            if (!error) await loadView(repo);
          } catch (e) {
            failFor(repo, e);
          } finally {
            patch({ busy: false });
          }
        })();
      },
      closeDialog: () => patch({ dialog: null, ask: null }),
      align: () => {
        const repo = ref.current.repo;
        if (!repo || !canWrite()) return;
        patch({ busy: true });
        void (async () => {
          const target = baseName(repo);
          try {
            const alignReport = await rpc(host, 'align', { repo });
            if (!current(repo)) {
              // the user moved to another repository: its page does not get this report
              patch({ notice: { kind: 'align', target, error: null, ok: true, reason: '', logError: alignReport.logError } });
              return;
            }
            patch({ alignReport, dialog: 'align' });
            await loadView(repo);
            if (!alignReport.fetch.ok) patch({ fetchError: alignReport.fetch.error || 'fetch' });
          } catch (e) {
            if (current(repo)) fail(e);
            else if (!noteGit(e)) patch({ notice: { kind: 'align', target, error: e, ok: false, reason: '', logError: '' } });
          } finally {
            patch({ busy: false });
          }
        })();
      },
      moveTrunk: (name, index) => {
        const repo = ref.current.repo;
        if (!repo || !canWrite()) return;
        patch({ busy: true });
        void (async () => {
          try {
            await rpc(host, 'moveTrunk', { repo, name, index });
            await loadView(repo);
          } catch (e) {
            fail(e);
          } finally {
            patch({ busy: false });
          }
        })();
      },
      cycleTheme: () => {
        const now = ref.current.global?.theme ?? 'auto';
        void writeGlobal({ theme: THEME_ORDER[(THEME_ORDER.indexOf(now) + 1) % THEME_ORDER.length] });
      },
      setEditor: (editor) => void writeGlobal({ editor }),
      setTheme: (theme) => void writeGlobal({ theme }),
      openWide: () => host.openWide(),
    };
    return { actions, start, reread, discovered };
  }, [host]);

  useEffect(() => {
    host.onTheme((theme) => {
      ref.current = { ...ref.current, theme };
      setState(ref.current);
    });
    host.onRefresh?.(() => void reread());
    host.onDiscovery?.(discovered);
    void start();
  }, [host, start, reread, discovered]);

  return { state, actions };
}
