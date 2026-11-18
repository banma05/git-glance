import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import { rpc } from './api';
import { HostError } from './host/types';
import type { Host, ThemeKind } from './host/types';
import type { AlignReport, Global, Theme, View } from './types';

export type ViewMode = 'list' | 'graph';
export type DialogName = 'align' | 'create' | 'compare' | 'oplog' | 'settings' | 'remove';

export interface AppState {
  repos: string[];
  repo: string;
  view: View | null;
  loaded: boolean; // the repository list (and the first view when there is a repository) arrived
  fetching: boolean;
  fetchError: string; // "" when the last read of the remote worked
  error: unknown; // a failed call other than the remote read; null when none
  noToken: boolean;
  mode: ViewMode;
  selected: string; // branch name, "" when none
  collapsed: Record<string, boolean>; // group key -> folded
  dialog: DialogName | null;
  alignReport: AlignReport | null;
  busy: boolean; // a write is running
  global: Global | null;
  theme: ThemeKind;
}

export interface Actions {
  selectRepo(repo: string): void;
  addRepo(path: string): void;
  reload(): void; // open the current repository again (after a failed first view)
  refresh(): void; // read the remote again, then the view (the retry button)
  setMode(mode: ViewMode): void;
  select(branch: string): void;
  toggleGroup(key: string): void;
  openDialog(name: DialogName): void;
  closeDialog(): void;
  align(): void;
  moveTrunk(name: string, index: number): void;
  cycleTheme(): void;
  setEditor(editor: string): void;
  openWide(): void;
}

const THEME_ORDER: Theme[] = ['auto', 'dark', 'light'];

// The group keys the list folds: "trunk:<name>", "unknown" and "old".
export const trunkKey = (name: string) => `trunk:${name}`;

function initial(host: Host): AppState {
  return {
    repos: [],
    repo: '',
    view: null,
    loaded: false,
    fetching: false,
    fetchError: '',
    error: null,
    noToken: false,
    mode: 'list',
    selected: '',
    collapsed: {},
    dialog: null,
    alignReport: null,
    busy: false,
    global: null,
    theme: host.theme(),
  };
}

// useApp holds the state of the whole screen and the actions on it. Every call
// goes through the host; nothing repeats on its own: the remote is read when a
// repository opens and when the user asks again.
export function useApp(host: Host): { state: AppState; actions: Actions } {
  const [state, setState] = useState<AppState>(() => initial(host));
  const ref = useRef(state);

  const { actions, start } = useMemo(() => {
    // patch updates the latest state at once, so the next step of a running
    // action reads what the previous step wrote.
    const patch = (p: Partial<AppState>) => {
      ref.current = { ...ref.current, ...p };
      setState(ref.current);
    };
    const fail = (e: unknown) => {
      if (e instanceof HostError && e.code === 'no-token') patch({ noToken: true, loaded: true });
      else patch({ error: e, loaded: true });
    };
    const current = (repo: string) => ref.current.repo === repo;

    // failFor reports a failure of a call made for `repo`; once the user has moved
    // to another repository the failure is not theirs to see.
    const failFor = (repo: string, e: unknown) => {
      if (current(repo)) fail(e);
    };
    const reasonOf = (e: unknown) => (e instanceof Error && e.message ? e.message : String(e));

    const loadView = async (repo: string) => {
      const view = await rpc(host, 'view', { repo });
      if (current(repo)) patch({ view, fetchError: view.fetchError, error: null });
      return view;
    };

    // refresh reads the remote in the background, then the view again. A failed
    // call lands in the "could not read the remote" line, which has the retry.
    const refresh = async (repo: string) => {
      patch({ fetching: true });
      try {
        const f = await rpc(host, 'fetch', { repo });
        const view = await rpc(host, 'view', { repo });
        if (!current(repo)) return;
        const failed = f.ok ? view.fetchError : f.error || view.fetchError || 'unknown';
        patch({ view, fetching: false, fetchError: failed, error: null });
      } catch (e) {
        if (!current(repo)) return;
        if (e instanceof HostError && e.code === 'no-token') fail(e);
        else patch({ fetchError: reasonOf(e) });
        patch({ fetching: false });
      }
    };

    const open = async (repo: string) => {
      // fetching is reset here: a refresh of the repository left behind drops its result
      patch({ repo, view: null, selected: '', fetchError: '', error: null, alignReport: null, fetching: false });
      try {
        await loadView(repo);
        if (!current(repo)) return;
        patch({ loaded: true });
      } catch (e) {
        failFor(repo, e);
        return;
      }
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
      try {
        patch({ global: await rpc(host, 'setGlobal', params), theme: host.theme() });
      } catch (e) {
        fail(e);
      }
    };

    const start = async () => {
      try {
        const repos = await rpc(host, 'repos');
        patch({ repos });
        if (repos.length === 0) patch({ loaded: true });
        else await open(repos[0]);
      } catch (e) {
        fail(e);
      }
    };
    const actions: Actions = {
      selectRepo: (repo) => {
        if (repo !== ref.current.repo) void open(repo);
      },
      addRepo: (path) => {
        void (async () => {
          try {
            const repos = await rpc(host, 'addRepo', { path });
            patch({ repos });
            const added = repos.find((r) => r.replace(/[\\/]+$/, '') === path.replace(/[\\/]+$/, '')) ?? repos[repos.length - 1];
            if (added) await open(added);
          } catch (e) {
            fail(e);
          }
        })();
      },
      reload: () => {
        if (ref.current.repo) void open(ref.current.repo);
      },
      refresh: () => {
        if (ref.current.repo && !ref.current.fetching) void refresh(ref.current.repo);
      },
      setMode: (mode) => patch({ mode }),
      select: (selected) => patch({ selected: ref.current.selected === selected ? '' : selected }),
      toggleGroup: (key) => patch({ collapsed: { ...ref.current.collapsed, [key]: !ref.current.collapsed[key] } }),
      openDialog: (dialog) => patch({ dialog }),
      closeDialog: () => patch({ dialog: null }),
      align: () => {
        const repo = ref.current.repo;
        if (!repo || ref.current.busy) return;
        patch({ busy: true });
        void (async () => {
          try {
            const alignReport = await rpc(host, 'align', { repo });
            patch({ alignReport, dialog: 'align' });
            await loadView(repo);
            if (!alignReport.fetch.ok) patch({ fetchError: alignReport.fetch.error || 'fetch' });
          } catch (e) {
            fail(e);
          } finally {
            patch({ busy: false });
          }
        })();
      },
      moveTrunk: (name, index) => {
        const repo = ref.current.repo;
        if (!repo || ref.current.busy) return;
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
      openWide: () => host.openWide(),
    };
    return { actions, start };
  }, [host]);

  useEffect(() => {
    host.onTheme((theme) => {
      ref.current = { ...ref.current, theme };
      setState(ref.current);
    });
    void start();
  }, [host, start]);

  return { state, actions };
}
