import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/preact';
import { App } from './App';
import { HostError } from '../host/types';
import type { Host } from '../host/types';
import { mockView } from '../host/mock';
import type { FetchResult } from '../types';

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

const core = (calls: string[]) => calls.filter((c) => c !== 'repos' && c !== 'getGlobal');

afterEach(cleanup);

describe('App', () => {
  it('calls view, then fetch, then view', async () => {
    const { host, calls } = fakeHost();
    render(<App host={host} />);
    await waitFor(() => expect(core(calls)).toEqual(['view', 'fetch', 'view']));
    expect(await screen.findByText('feature/login')).toBeTruthy();
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
    expect(screen.queryByText('feature/login')).toBeNull();
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
    expect(await screen.findByText('feature/login')).toBeTruthy();
    expect(screen.queryByRole('alert')).toBeNull();
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
