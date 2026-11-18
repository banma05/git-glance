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
