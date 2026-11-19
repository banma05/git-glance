import * as fs from 'node:fs';
import * as path from 'node:path';
import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/preact';
import { App } from './App';
import { makeT } from '../i18n';
import { HostError } from '../host/types';
import type { Host } from '../host/types';
import { mockView } from '../host/mock';

// named finds a branch name, which BranchName writes in two parts.
const named = (n: string) => (_: string, el: Element | null) =>
  !!el?.classList.contains('bname') &&
  (el.querySelector('.bname-pre')?.textContent ?? '') + el.querySelector('.bname-last')!.textContent === n;


const en = makeT('en');
const zh = makeT('zh-cn');

function sidebarHost(over: Record<string, unknown> = {}, locale = 'en') {
  const calls: Array<{ method: string; params: Record<string, unknown> }> = [];
  let repos: string[] = [];
  const host: Host = {
    async call<T>(method: string, params?: object): Promise<T> {
      const p = (params ?? {}) as Record<string, unknown>;
      calls.push({ method, params: p });
      const v = over[method];
      if (v instanceof Error) throw v;
      if (v !== undefined) return (typeof v === 'function' ? (v as (p: unknown) => unknown)(p) : v) as T;
      switch (method) {
        case 'repos':
          return repos as T;
        case 'addRepo':
          repos = [String(p.path)];
          return repos as T;
        case 'discover':
          return { repo: '/found/repo' } as T;
        case 'view':
          return mockView(String(p.repo)) as T;
        case 'fetch':
          return { ok: true, attempts: 1, error: '', at: 1 } as T;
        case 'getGlobal':
          return { repos, editor: 'code', theme: 'auto' } as T;
        case 'version':
          return { core: 'x', git: '2.45', gitOK: true } as T;
        default:
          throw new HostError('unknown-method', method);
      }
    },
    openFolder: async () => undefined,
    copy: async () => undefined,
    openDiff: async () => false,
    openWide: () => undefined,
    mode: 'sidebar',
    locale,
    theme: () => 'dark',
    onTheme: () => undefined,
  };
  return { host, calls };
}

afterEach(cleanup);

describe('sidebar', () => {
  it('has the "+" the empty state points to, and adds a repository with it', async () => {
    const { host, calls } = sidebarHost();
    render(<App host={host} />);
    expect(await screen.findByText(en('repo.none'))).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: en('repo.add') }));
    fireEvent.input(screen.getByLabelText(en('repo.addPrompt')), { target: { value: '/some/sub/folder' } });
    fireEvent.click(screen.getByRole('button', { name: en('repo.addSubmit') }));
    await waitFor(() => expect(calls.find((c) => c.method === 'addRepo')?.params).toEqual({ path: '/found/repo' }));
    expect(calls.find((c) => c.method === 'discover')?.params).toEqual({ path: '/some/sub/folder' });
    expect(await screen.findByText(named('feature/login'))).toBeTruthy();
  });

  it('shows the empty state right under its header, not at the bottom', async () => {
    const { host } = sidebarHost({}, 'zh-cn');
    const { container } = render(<App host={host} />);
    await screen.findByText(zh('repo.none'));
    const head = container.querySelector('.sidebar > .side-head');
    expect(head).toBeTruthy();
    expect(head!.nextElementSibling?.textContent).toBe(zh('repo.none'));
  });

  it('keeps the status lines under its header too', async () => {
    const { host } = sidebarHost({ repos: ['/demo/repo'], fetch: { ok: false, attempts: 2, error: 'offline', at: 0 } });
    const { container } = render(<App host={host} />);
    await screen.findByText(/offline/);
    const head = container.querySelector('.sidebar > .side-head');
    expect(head!.nextElementSibling?.textContent).toContain('offline');
  });
});

describe('narrow layout rules', () => {
  const css = fs.readFileSync(path.resolve(__dirname, '../app.css'), 'utf8');
  const rule = (selector: string) => {
    const at = css.indexOf(`\n${selector} {`);
    expect(at, selector).toBeGreaterThanOrEqual(0);
    return css.slice(at, css.indexOf('}', at));
  };

  it('never wraps the text of a toggle button', () => {
    expect(rule('.seg .btn')).toMatch(/white-space:\s*nowrap/);
  });

  it('lets the sidebar header wrap instead of overflowing', () => {
    expect(rule('.side-head')).toMatch(/flex-wrap:\s*wrap/);
  });

  it('never scrolls the sidebar sideways', () => {
    expect(rule('.sidebar')).toMatch(/overflow-x:\s*hidden/);
  });

  it('keeps dialogs and their inputs inside a narrow window', () => {
    expect(rule('.dialog')).toMatch(/min-width:\s*min\(/);
    expect(rule('.dialog')).toMatch(/max-width:\s*calc\(100vw/);
  });
});
