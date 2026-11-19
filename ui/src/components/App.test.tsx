import * as fs from 'node:fs';
import * as path from 'node:path';
import { afterEach, describe, expect, it } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/preact';
import { App } from './App';
import { HostError } from '../host/types';
import type { Host } from '../host/types';
import { mockView } from '../host/mock';
import type { FetchResult } from '../types';

// named finds a branch name, which BranchName writes in two parts.
const named = (n: string) => (_: string, el: Element | null) =>
  !!el?.classList.contains('bname') &&
  (el.querySelector('.bname-pre')?.textContent ?? '') + el.querySelector('.bname-last')!.textContent === n;


function fakeHost(over: Record<string, unknown> = {}, locale = 'en') {
  const calls: string[] = [];
  const host: Host = {
    async call<T>(method: string): Promise<T> {
      calls.push(method);
      const v = over[method];
      if (v instanceof Error) throw v;
      if (v !== undefined) return (typeof v === 'function' ? (v as () => unknown)() : v) as T;
      switch (method) {
        case 'repos':
          return ['/demo/repo'] as T;
        case 'view':
          return mockView('/demo/repo') as T;
        case 'fetch':
          return { ok: true, attempts: 1, error: '', at: 1 } as FetchResult as T;
        case 'getGlobal':
          return { repos: ['/demo/repo'], editor: 'code', theme: 'auto' } as T;
        default:
          throw new HostError('unknown-method', method);
      }
    },
    openFolder: async () => undefined,
    copy: async () => undefined,
    openDiff: async () => false,
    openWide: () => undefined,
    mode: 'wide',
    locale,
    theme: () => 'dark',
    onTheme: () => undefined,
  };
  return { host, calls };
}

const core = (calls: string[]) => calls.filter((c) => c !== 'repos' && c !== 'getGlobal' && c !== 'version');

afterEach(cleanup);

// once returns the given values in turn, the last one for good; an Error is thrown.
const once = (...values: unknown[]) => {
  let n = 0;
  return () => {
    const v = values[Math.min(n++, values.length - 1)];
    if (v instanceof Error) throw v;
    return v;
  };
};

describe('the repository list', () => {
  it('shows a failed list as an error with Retry, not as "no repository"', async () => {
    const { host, calls } = fakeHost({ repos: once(new HostError('timeout', 'repos did not answer within 60 s'), ['/demo/repo']) });
    render(<App host={host} />);
    expect(await screen.findByText(/The core did not answer in time/)).toBeTruthy();
    expect(screen.queryByText(/No repository yet/)).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(await screen.findByText(named('feature/login'))).toBeTruthy();
    expect(calls.filter((c) => c === 'repos')).toHaveLength(2);
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('shows "no repository" only when the list answered empty', async () => {
    const { host } = fakeHost({ repos: [] });
    render(<App host={host} />);
    expect(await screen.findByText('No repository yet; click + to add one')).toBeTruthy();
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('says why a workspace folder could not be added instead of the plain empty state', async () => {
    let tell: (p: unknown) => void = () => undefined;
    const { host } = fakeHost({ repos: [] });
    host.onDiscovery = (cb) => (tell = cb as (p: unknown) => void);
    const realCall = host.call.bind(host);
    host.call = async <T,>(method: string, params?: object): Promise<T> => {
      if (method === 'repos') tell([{ folder: '/w/project', code: 'core-unavailable', message: 'it stopped right after starting' }]);
      return realCall<T>(method, params);
    };
    render(<App host={host} />);
    const line = await screen.findByText(/\/w\/project/);
    expect(line.textContent).toContain('The core program could not be started');
    expect(line.textContent).toContain('Git Glance: Refresh');
    expect(screen.queryByText(/No repository yet/)).toBeNull();
  });

  it('drops the alert of a folder once "+" added its repository, and keeps the others', async () => {
    let tell: (p: unknown) => void = () => undefined;
    let listed: string[] = [];
    const { host } = fakeHost({
      repos: () => listed,
      discover: { repo: 'C:/w/project' },
      addRepo: () => (listed = ['C:/w/project']),
    });
    host.onDiscovery = (cb) => (tell = cb as (p: unknown) => void);
    const realCall = host.call.bind(host);
    host.call = async <T,>(method: string, params?: object): Promise<T> => {
      if (method === 'repos')
        tell([
          { folder: 'C:\\w\\project\\app', code: 'core-unavailable', message: 'x' },
          { folder: '/w/other', code: 'core-unavailable', message: 'y' },
        ]);
      return realCall<T>(method, params);
    };
    render(<App host={host} />);
    expect(await screen.findByText(/w\\project\\app/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Add repository' }));
    fireEvent.input(screen.getByLabelText('Full path of the repository folder'), { target: { value: '/w/project/app' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add' }));
    expect(await screen.findByText(named('feature/login'))).toBeTruthy();
    expect(screen.queryByText(/w\\project\\app/)).toBeNull();
    expect(screen.getByText(/\/w\/other/)).toBeTruthy();
  });

  it('does not point to a disabled "+" when git is too old', async () => {
    const { host } = fakeHost({ repos: [], version: { core: 'x', git: '2.29', gitOK: false } });
    render(<App host={host} />);
    expect(await screen.findByText(/needs git 2\.30 or newer to add one/)).toBeTruthy();
    expect(screen.queryByText(/click \+/)).toBeNull();
    expect(screen.queryByText(/Reading still works/)).toBeNull();
  });

  it('never shows a cancelled call', async () => {
    const { host } = fakeHost({ view: once(new HostError('cancelled', 'cancelled'), mockView('/demo/repo')) });
    render(<App host={host} />);
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByRole('alert')).toBeNull();
    expect(screen.queryByText(/cancelled/i)).toBeNull();
  });

  it('says a read of the remote that ran out of time goes on, not that it failed', async () => {
    const { host } = fakeHost({ fetch: new HostError('still-running', 'fetch is still running after 20 min') });
    render(<App host={host} />);
    expect(await screen.findByText(/Reading the remote takes long/)).toBeTruthy();
    expect(screen.queryByText(/Could not read the remote/)).toBeNull();
  });
});

describe('the list beside the graph in the wide window', () => {
  it('shows "feature/login" whole, folders included', async () => {
    const { host } = fakeHost();
    const { container } = render(<App host={host} />);
    await screen.findByText(named('feature/login'));
    fireEvent.click(screen.getByRole('button', { name: 'Graph' }));
    const left = container.querySelector('.left')!;
    const row = [...left.querySelectorAll('.row')].find((r) => r.getAttribute('title') === 'alice/feature/login')!;
    const name = row.querySelector('.bname')!;
    expect(name.querySelector('.bname-pre')!.textContent).toBe('feature');
    expect(name.querySelector('.bname-last')!.textContent).toBe('/login');
    // nothing hides the folders by width (see BranchName.test.tsx: they only wrap away when the name does not fit)
    expect(name.querySelector('.bname-pre')!.getAttribute('style')).toBeNull();
  });
});

describe('git too old', () => {
  const byName = (name: string) => screen.getByRole('button', { name: new RegExp(name) }) as HTMLButtonElement;

  it('says so under the toolbar and disables the buttons that change something', async () => {
    const { host } = fakeHost({ version: { core: '0.1.0', git: '2.29', gitOK: false } });
    render(<App host={host} />);
    expect(await screen.findByText(/Git 2\.29 is too old/)).toBeTruthy();
    await screen.findByText(named('feature/login'));
    expect(byName('Align all with remote').disabled).toBe(true);
    expect(byName('New requirement folder').disabled).toBe(true);
    expect(byName('Compare two branches').disabled).toBe(false);
    fireEvent.click(screen.getByText(named('feature/search')));
    expect(byName('Switch here').disabled).toBe(true);
  });

  it('does not read the remote (a change for the core) and shows no fetch error', async () => {
    const { host, calls } = fakeHost({
      version: { core: '0.1.0', git: '2.29', gitOK: false },
      fetch: new HostError('git-too-old', 'changes need git 2.30 or newer'),
    });
    render(<App host={host} />);
    await screen.findByText(named('feature/login'));
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByText(/Could not read the remote/)).toBeNull();
    expect(screen.getAllByRole('alert')).toHaveLength(1);
    expect(calls.filter((c) => c === 'fetch').length).toBeLessThanOrEqual(1);
  });

  it('says git was not found when there is no git', async () => {
    const { host } = fakeHost({ version: { core: '0.1.0', git: '', gitOK: false } });
    render(<App host={host} />);
    expect(await screen.findByText(/Git was not found/)).toBeTruthy();
  });

  it('shows the same line when a write is refused with git-too-old', async () => {
    const { host } = fakeHost({
      version: { core: '0.1.0', git: '2.45', gitOK: true },
      align: new HostError('git-too-old', 'changes need git 2.30 or newer'),
    });
    render(<App host={host} />);
    await screen.findByText(named('feature/login'));
    expect(screen.queryByText(/too old/)).toBeNull();
    fireEvent.click(byName('Align all with remote'));
    expect(await screen.findByText(/Git is too old/)).toBeTruthy();
    await waitFor(() => expect(byName('Align all with remote').disabled).toBe(true));
  });
});

describe('App', () => {
  it('calls view, then fetch, then view', async () => {
    const { host, calls } = fakeHost();
    render(<App host={host} />);
    await waitFor(() => expect(core(calls)).toEqual(['view', 'fetch', 'view']));
    expect(await screen.findByText(named('feature/login'))).toBeTruthy();
  });

  it('shows a retry line when the fetch fails and fetches again on click', async () => {
    const { host, calls } = fakeHost({ fetch: { ok: false, attempts: 2, error: 'x', at: 0 } });
    render(<App host={host} />);
    const retry = await screen.findByRole('button', { name: 'Retry' });
    expect(screen.getByText(/Could not read the remote: x/)).toBeTruthy();
    const before = calls.filter((c) => c === 'fetch').length;
    fireEvent.click(retry);
    await waitFor(() => expect(calls.filter((c) => c === 'fetch').length).toBe(before + 1));
    await waitFor(() => expect(core(calls).slice(-2)).toEqual(['fetch', 'view']));
  });

  it('shows the error of the view line when fetchError is set', async () => {
    const view = { ...mockView('/demo/repo'), fetchError: 'offline' };
    const { host } = fakeHost({ view });
    render(<App host={host} />);
    expect(await screen.findByText(/offline/)).toBeTruthy();
  });

  for (const [locale, text] of [
    ['en', 'Open the link printed in the terminal'],
    ['zh-cn', '请打开终端里打印出来的链接'],
  ] as const) {
    it(`shows the no-token message in ${locale}`, async () => {
      const { host } = fakeHost({ repos: new HostError('no-token', 'no access token') }, locale);
      render(<App host={host} />);
      expect(await screen.findByText(text)).toBeTruthy();
      expect(screen.getByText(/gitglance serve/)).toBeTruthy();
    });
  }
});

// A host whose answers are chosen per call: handler(method, repo) returns a value,
// a promise or throws.
function scriptedHost(handler: (method: string, repo: string, n: number) => unknown) {
  const calls: string[] = [];
  const counts: Record<string, number> = {};
  const host: Host = {
    async call<T>(method: string, params?: object): Promise<T> {
      const repo = String((params as { repo?: string } | undefined)?.repo ?? '');
      const k = `${method}:${repo}`;
      counts[k] = (counts[k] ?? 0) + 1;
      calls.push(k);
      if (method === 'repos') return ['/a', '/b'] as T;
      if (method === 'getGlobal') return { repos: ['/a', '/b'], editor: 'code', theme: 'auto' } as T;
      if (method === 'version') return { core: '0.1.0', git: '2.45', gitOK: true } as T;
      return (await handler(method, repo, counts[k])) as T;
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
  return { host, calls, counts };
}
const okFetch = { ok: true, attempts: 1, error: '', at: 1 };

describe('App failures and repo switching', () => {
  it('keeps refresh state per repo when the user switches mid-refresh', async () => {
    let lateFetch!: (v: unknown) => void;
    const pending = new Promise((r) => (lateFetch = r));
    const { host } = scriptedHost((method, repo) => {
      if (method === 'view' && repo === '/a') return mockView('/a');
      if (method === 'fetch' && repo === '/a') return pending;
      if (method === 'view' && repo === '/b') throw new HostError('git-failed', 'exit 1');
      throw new HostError('unknown-method', method);
    });
    render(<App host={host} />);
    await screen.findByText('Reading the remote…');
    fireEvent.click(screen.getByRole('tab', { name: 'b' }));
    expect(await screen.findByText(/A git command failed/)).toBeTruthy();
    lateFetch({ ok: false, attempts: 1, error: 'late-from-a', at: 0 });
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByText(/late-from-a/)).toBeNull();
    expect(screen.queryByText('Reading the remote…')).toBeNull();
    expect(screen.queryByText(named('feature/login'))).toBeNull();
    const retry = screen.getByRole('button', { name: 'Retry' }) as HTMLButtonElement;
    expect(retry.disabled).toBe(false);
  });

  it('does not show repo A failure on repo B', async () => {
    let failFetch!: (e: unknown) => void;
    const pending = new Promise((_r, rej) => (failFetch = rej));
    const { host } = scriptedHost((method, repo) => {
      if (method === 'fetch' && repo === '/a') return pending;
      if (method === 'view') return mockView(repo);
      return okFetch;
    });
    render(<App host={host} />);
    await screen.findByText('Reading the remote…');
    fireEvent.click(screen.getByRole('tab', { name: 'b' }));
    await waitFor(() => expect(screen.queryByText('Reading the remote…')).toBeNull());
    failFetch(new HostError('network', 'a-is-down'));
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByText(/a-is-down/)).toBeNull();
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('shows the retry line for a rejected fetch and retries fetch then view', async () => {
    const { host, calls } = scriptedHost((method, _repo, n) => {
      if (method === 'view') return mockView('/a');
      if (method === 'fetch') {
        if (n === 1) throw new HostError('network', 'offline');
        return okFetch;
      }
      throw new HostError('unknown-method', method);
    });
    render(<App host={host} />);
    expect(await screen.findByText(/Could not read the remote: offline/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await waitFor(() => expect(screen.queryByText(/Could not read the remote/)).toBeNull());
    expect(calls.filter((c) => !c.startsWith('repos') && !c.startsWith('getGlobal')).slice(-2)).toEqual(['fetch:/a', 'view:/a']);
  });

  it('shows an error with a working retry when the first view is rejected', async () => {
    const { host } = scriptedHost((method, _repo, n) => {
      if (method === 'view') {
        if (n === 1) throw new TypeError('Failed to fetch');
        return mockView('/a');
      }
      return okFetch;
    });
    render(<App host={host} />);
    expect(await screen.findByText(/Failed to fetch/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(await screen.findByText(named('feature/login'))).toBeTruthy();
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('reads the repository list again when the host says refresh, and opens the first one', async () => {
    let repos: string[] = [];
    let refresh: () => void = () => undefined;
    const { host, calls } = fakeHost({ repos: () => repos });
    host.onRefresh = (cb) => (refresh = cb);
    render(<App host={host} />);
    expect(await screen.findByText('No repository yet; click + to add one')).toBeTruthy();
    repos = ['/demo/repo'];
    await act(async () => refresh());
    expect(await screen.findByText(named('feature/login'))).toBeTruthy();
    expect(screen.queryByText('No repository yet; click + to add one')).toBeNull();
    expect(core(calls)).toEqual(['view', 'fetch', 'view']);
  });

  it('on refresh keeps the open repository and reads only its view again', async () => {
    let refresh: () => void = () => undefined;
    const { host, calls } = scriptedHost((method, repo) => (method === 'view' ? mockView(repo) : okFetch));
    host.onRefresh = (cb) => (refresh = cb);
    render(<App host={host} />);
    await screen.findByText(named('feature/login'));
    fireEvent.click(screen.getByRole('tab', { name: 'b' }));
    await waitFor(() => expect(calls.filter((c) => c === 'view:/b').length).toBe(2));
    const before = calls.length;
    await act(async () => refresh());
    await waitFor(() => expect(calls.slice(before)).toEqual(['repos:', 'view:/b']));
    expect(screen.getByRole('tab', { name: 'b' }).getAttribute('aria-selected')).toBe('true');
  });

  it('does not show the align report of repository a over repository b', async () => {
    let finish!: (v: unknown) => void;
    const report = { fetch: okFetch, updated: [{ branch: 'from-a', from: 'a1', to: 'a2', folder: '' }], skipped: [], logError: '' };
    const { host } = scriptedHost((method, repo) => {
      if (method === 'align') return new Promise((r) => (finish = r));
      return method === 'view' ? mockView(repo) : okFetch;
    });
    render(<App host={host} />);
    await screen.findByText(named('feature/login'));
    fireEvent.click(screen.getByRole('button', { name: /Align all with remote/ }));
    fireEvent.click(screen.getByRole('tab', { name: 'b' }));
    await act(async () => finish(report));
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(screen.queryByText(/from-a/)).toBeNull();
    expect(screen.getByTestId('notice').textContent).toContain('Align with remote - a: done');
  });

  it('drops a trunk commit pick when the mode changes', async () => {
    const { host } = scriptedHost((method, repo) => (method === 'view' ? mockView(repo) : okFetch));
    const { container } = render(<App host={host} />);
    await screen.findByText(named('feature/login'));
    fireEvent.click(screen.getByRole('button', { name: 'Graph' }));
    fireEvent.click(container.querySelector('.g-row[data-kind="head"]')!);
    expect(await screen.findByText('Commit on the trunk')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'List' }));
    expect(screen.queryByText('Commit on the trunk')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Graph' }));
    expect(screen.queryByText('Commit on the trunk')).toBeNull();
    expect(container.querySelector('.g-ring')).toBeNull();
  });

  it('drops a selection whose branch is gone from a view read again', async () => {
    let refresh: () => void = () => undefined;
    let gone = false;
    const { host } = scriptedHost((method, repo) => {
      if (method !== 'view') return okFetch;
      const v = mockView(repo);
      if (gone) {
        delete v.branches['carol/feature/search'];
        v.groups[0].branches = v.groups[0].branches.filter((b) => b !== 'carol/feature/search');
      }
      return v;
    });
    host.onRefresh = (cb) => (refresh = cb);
    render(<App host={host} />);
    fireEvent.click(await screen.findByText(named('feature/search')));
    expect(await screen.findByRole('button', { name: 'Switch here' })).toBeTruthy();
    gone = true;
    await act(async () => refresh());
    await waitFor(() => expect(screen.queryByText(named('feature/search'))).toBeNull());
    expect(screen.queryByRole('button', { name: 'Switch here' })).toBeNull();
    expect(screen.getByText('Click a row to see details')).toBeTruthy();
    // the selection went with the branch: a later view that has it again does not bring it back
    gone = false;
    await act(async () => refresh());
    await screen.findByText(named('feature/search'));
    expect(screen.queryByRole('button', { name: 'Switch here' })).toBeNull();
  });

  it('lets the latest read of the list win: a late empty answer never clears what a later read opened', async () => {
    let refresh: () => void = () => undefined;
    let first!: (v: unknown) => void;
    let n = 0;
    const host: Host = {
      async call<T>(method: string, params?: object): Promise<T> {
        const repo = String((params as { repo?: string } | undefined)?.repo ?? '');
        if (method === 'repos') return (++n === 1 ? new Promise((r) => (first = r as (v: unknown) => void)) : ['/a']) as Promise<T>;
        if (method === 'view') return mockView(repo) as T;
        if (method === 'getGlobal') return { repos: ['/a'], editor: 'code', theme: 'auto' } as T;
        if (method === 'version') return { core: 'x', git: '2.45', gitOK: true } as T;
        return okFetch as T;
      },
      openFolder: async () => undefined,
      copy: async () => undefined,
      openDiff: async () => false,
      openWide: () => undefined,
      mode: 'wide',
      locale: 'en',
      theme: () => 'dark',
      onTheme: () => undefined,
      onRefresh: (cb) => (refresh = cb),
    };
    render(<App host={host} />);
    await waitFor(() => expect(n).toBe(1));
    await act(async () => refresh());
    expect(await screen.findByText(named('feature/login'))).toBeTruthy();
    await act(async () => first([]));
    await new Promise((r) => setTimeout(r, 30));
    expect(screen.getByText(named('feature/login'))).toBeTruthy();
    expect(screen.queryByText(/No repository yet/)).toBeNull();
  });

  it('makes no further calls once the opening sequence has settled', async () => {
    const { host, calls } = scriptedHost((method, repo) => (method === 'view' ? mockView(repo) : okFetch));
    render(<App host={host} />);
    await waitFor(() => expect(calls).toContain('getGlobal:'));
    const settled = calls.length;
    await new Promise((r) => setTimeout(r, 300));
    expect(calls.length).toBe(settled);
  });
});

describe('trunk details', () => {
  // the name of a trunk group's header, "Trunk main (4)" (jsdom drops the space
  // before the count); the graph's title is "Trunk main"
  const headName = (trunk: string) => screen.getByRole('button', { name: new RegExp(`^Trunk ${trunk} ?\\(\\d+\\)$`) });
  const arrow = (trunk: string) => screen.getByRole('button', { name: `Show or hide the branches under ${trunk}` });
  const pressed = (el: Element) => el.getAttribute('aria-pressed');

  it('shows a trunk when its name in the list is clicked; the arrow only folds', async () => {
    const { host } = fakeHost();
    render(<App host={host} />);
    await screen.findByText(named('feature/login'));
    expect(arrow('main').tagName).toBe('BUTTON');
    expect(headName('main').tagName).toBe('BUTTON');
    expect(arrow('main').getAttribute('aria-expanded')).toBe('true');
    expect(pressed(headName('main'))).toBe('false');

    fireEvent.click(arrow('main'));
    expect(arrow('main').getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByText(named('feature/login'))).toBeNull();
    expect(pressed(headName('main'))).toBe('false');
    expect(screen.queryByText('Compared through')).toBeNull();
    fireEvent.click(arrow('main'));

    fireEvent.click(headName('main'));
    expect(pressed(headName('main'))).toBe('true');
    expect(headName('main').closest('.group-head')!.classList.contains('selected')).toBe(true);
    expect(screen.getByText('Compared through')).toBeTruthy();
    expect(screen.getByText('origin/main')).toBeTruthy();
    expect(arrow('main').getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByText(named('feature/login'))).toBeTruthy();

    fireEvent.click(headName('main'));
    expect(pressed(headName('main'))).toBe('false');
    expect(screen.getByText('Click a row to see details')).toBeTruthy();
  });

  it('never has a trunk and a branch selected at once', async () => {
    const { host } = fakeHost();
    const { container } = render(<App host={host} />);
    await screen.findByText(named('feature/login'));
    fireEvent.click(headName('main'));
    fireEvent.click(screen.getByText(named('feature/search')));
    expect(pressed(headName('main'))).toBe('false');
    expect(screen.getByRole('button', { name: 'Switch here' })).toBeTruthy();
    fireEvent.click(headName('develop'));
    expect(container.querySelector('.row.selected')).toBeNull();
    expect(screen.getByText('Behind the remote by 3')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Switch here' })).toBeNull();
  });

  it('shows a trunk from the graph title, and keeps it across the list and the graph', async () => {
    const { host } = fakeHost();
    const { container } = render(<App host={host} />);
    await screen.findByText(named('feature/login'));
    fireEvent.click(screen.getByRole('button', { name: 'Graph' }));
    const title = () => screen.getByRole('button', { name: 'Trunk main' });
    expect(pressed(title())).toBe('false');
    fireEvent.click(title());
    expect(pressed(title())).toBe('true');
    expect(pressed(headName('main'))).toBe('true'); // the list beside the graph
    expect(screen.getByText('Compared through')).toBeTruthy();

    fireEvent.click(container.querySelector('.g-row[data-kind="head"]')!);
    expect(await screen.findByText('Commit on the trunk')).toBeTruthy();
    expect(pressed(title())).toBe('false');

    fireEvent.click(title());
    fireEvent.click(screen.getByRole('button', { name: 'List' }));
    expect(pressed(headName('main'))).toBe('true');
    expect(screen.getByText('Compared through')).toBeTruthy();
  });

  it('opens Compare with the trunk on the first side', async () => {
    const { host } = fakeHost();
    render(<App host={host} />);
    await screen.findByText(named('feature/login'));
    fireEvent.click(headName('develop'));
    fireEvent.click(screen.getByRole('button', { name: 'Compare with…' }));
    const a = (await screen.findByLabelText('Branch A')) as HTMLSelectElement;
    expect(a.value).toBe('develop');
  });

  it('keeps the trunk on a refresh, drops it when the trunk is gone, and on another repository', async () => {
    let refresh: () => void = () => undefined;
    let gone = false;
    const { host } = scriptedHost((method, repo) => {
      if (method !== 'view') return okFetch;
      const v = mockView(repo);
      if (gone) v.groups = v.groups.filter((g) => g.trunk.name !== 'develop');
      return v;
    });
    host.onRefresh = (cb) => (refresh = cb);
    render(<App host={host} />);
    await screen.findByText(named('feature/login'));
    fireEvent.click(headName('develop'));
    await act(async () => refresh());
    expect(pressed(headName('develop'))).toBe('true');
    expect(screen.getByText('Compared through')).toBeTruthy();

    gone = true;
    await act(async () => refresh());
    await waitFor(() => expect(screen.queryByText('Compared through')).toBeNull());
    expect(screen.getByText('Click a row to see details')).toBeTruthy();
    // the selection went with the trunk: a view that has it again does not bring it back
    gone = false;
    await act(async () => refresh());
    await waitFor(() => expect(pressed(headName('develop'))).toBe('false'));

    fireEvent.click(headName('main'));
    fireEvent.click(screen.getByRole('tab', { name: 'b' }));
    await waitFor(() => expect(screen.queryByText('Compared through')).toBeNull());
    await screen.findByText(named('feature/login'));
    expect(pressed(headName('main'))).toBe('false');
  });
});

describe('dragging a trunk header', () => {
  const css = fs.readFileSync(path.resolve(__dirname, '../app.css'), 'utf8');

  it('has a grab area of its own beside the name, which no longer fills the header', async () => {
    const style = document.createElement('style');
    style.textContent = css;
    document.head.appendChild(style);
    try {
      const { host } = fakeHost();
      const { container } = render(<App host={host} />);
      await screen.findByText(named('feature/login'));
      const head = container.querySelector('.trunk-head')!;
      expect(head.getAttribute('draggable')).toBe('true');
      expect(head.getAttribute('title')).toBe('Drag to reorder');
      expect(getComputedStyle(head).cursor).toBe('grab');
      const name = head.querySelector('.group-name')!;
      expect(name.getAttribute('title')).toBe('Click for the trunk details; drag to reorder');
      expect(getComputedStyle(name).flexGrow).toBe('0');
    } finally {
      style.remove();
    }
    // only a draggable header gives up room: the sidebar's name still fills its header
    const rule = (selector: string) => {
      const at = css.indexOf(`\n${selector} {`);
      expect(at, selector).toBeGreaterThanOrEqual(0);
      return css.slice(at, css.indexOf('}', at));
    };
    expect(rule(".trunk-head[draggable='true'] .group-name")).toMatch(/flex:\s*0 1 auto/);
    expect(rule(".trunk-head[draggable='true']::after")).toMatch(/content:/);
  });
});
