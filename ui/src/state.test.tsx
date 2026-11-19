import { afterEach, describe, expect, it } from 'vitest';
import { act, cleanup, render, waitFor } from '@testing-library/preact';
import { useApp } from './state';
import type { Actions, AppState } from './state';
import { mockView } from './host/mock';
import type { Host } from './host/types';

// harness renders useApp and hands out its latest state and actions.
function harness(view: (repo: string) => unknown = (repo) => mockView(repo)) {
  let app: { state: AppState; actions: Actions } | null = null;
  const host: Host = {
    async call<T>(method: string, params?: object): Promise<T> {
      const repo = String((params as { repo?: string } | undefined)?.repo ?? '');
      if (method === 'repos') return ['/a', '/b'] as T;
      if (method === 'view') return view(repo) as T;
      if (method === 'getGlobal') return { repos: ['/a', '/b'], editor: 'code', theme: 'auto' } as T;
      if (method === 'version') return { core: 'x', git: '2.45', gitOK: true } as T;
      return { ok: true, attempts: 1, error: '', at: 1 } as T;
    },
    openFolder: async () => undefined,
    copy: async () => undefined,
    openDiff: async () => false,
    openWide: () => undefined,
    mode: 'wide',
    locale: 'en',
    theme: () => 'dark',
    onTheme: () => undefined,
  };
  function Probe() {
    app = useApp(host);
    return null;
  }
  render(<Probe />);
  return () => app!;
}

const BRANCH = { kind: 'branch', branch: 'bob/fix/timeout' } as const;

afterEach(cleanup);

describe('the trunk news panel state', () => {
  it('closes on a repository switch and on reload, and stays when the view is read again', async () => {
    const app = harness();
    await waitFor(() => expect(app().state.view?.repo).toBe('/a'));
    act(() => app().actions.select('bob/fix/timeout'));
    act(() => app().actions.toggleNews(BRANCH));
    expect(app().state.news).toEqual(BRANCH);

    await act(async () => app().actions.refreshView());
    expect(app().state.news).toEqual(BRANCH);

    act(() => app().actions.reload());
    expect(app().state.news).toBeNull();
    await waitFor(() => expect(app().state.view?.repo).toBe('/a'));

    act(() => app().actions.select('bob/fix/timeout'));
    act(() => app().actions.toggleNews(BRANCH));
    act(() => app().actions.selectRepo('/b'));
    expect(app().state.news).toBeNull();
    await waitFor(() => expect(app().state.view?.repo).toBe('/b'));
    expect(app().state.news).toBeNull();
  });

  it('belongs to the selection: kept across list and graph, closed when another branch or trunk is selected', async () => {
    const app = harness();
    await waitFor(() => expect(app().state.view).not.toBeNull());
    act(() => app().actions.select('bob/fix/timeout'));
    act(() => app().actions.toggleNews(BRANCH));
    act(() => app().actions.setMode('graph'));
    expect(app().state.news).toEqual(BRANCH);
    act(() => app().actions.selectCommit('bob/fix/timeout', app().state.view!.branches['bob/fix/timeout'].own[0].hash));
    expect(app().state.news).toEqual(BRANCH); // a commit of the same branch
    act(() => app().actions.select('carol/feature/search'));
    expect(app().state.news).toBeNull();

    act(() => app().actions.showTrunk('main'));
    act(() => app().actions.toggleNews({ kind: 'trunk', trunk: 'main' }));
    expect(app().state.news).toEqual({ kind: 'trunk', trunk: 'main' });
    act(() => app().actions.toggleNews({ kind: 'trunk', trunk: 'main' }));
    expect(app().state.news).toBeNull();
    act(() => app().actions.toggleNews({ kind: 'trunk', trunk: 'main' }));
    act(() => app().actions.showTrunk('develop'));
    expect(app().state.news).toBeNull();
  });

  it('closes when its branch is gone from a view read again', async () => {
    let gone = false;
    const app = harness((repo) => {
      const v = mockView(repo);
      if (gone) delete v.branches['bob/fix/timeout'];
      return v;
    });
    await waitFor(() => expect(app().state.view).not.toBeNull());
    act(() => app().actions.select('bob/fix/timeout'));
    act(() => app().actions.toggleNews(BRANCH));
    gone = true;
    await act(async () => app().actions.refreshView());
    await waitFor(() => expect(app().state.selected).toBe(''));
    expect(app().state.news).toBeNull();
  });
});
