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

describe('sidebar trunk details', () => {
  const pressed = (el: Element) => el.getAttribute('aria-pressed');

  it('shows a trunk under its header in the list; the arrow only folds', async () => {
    const { host } = sidebarHost({ repos: ['/demo/repo'] });
    render(<App host={host} />);
    await screen.findByText(named('feature/login'));
    const name = screen.getByRole('button', { name: /^Trunk develop ?\(2\)$/ }); // jsdom drops the space
    fireEvent.click(name);
    expect(pressed(name)).toBe('true');
    const head = name.closest('.group-head')!;
    expect(head.nextElementSibling!.classList.contains('detail')).toBe(true);
    expect(head.nextElementSibling!.textContent).toContain('Behind the remote by 3');

    const arrow = screen.getByRole('button', { name: 'Show or hide the branches under develop' });
    fireEvent.click(arrow);
    expect(arrow.getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByText(named('feature/export'))).toBeNull();
    expect(pressed(name)).toBe('true'); // folding keeps the trunk shown
    expect(screen.getByText('Behind the remote by 3')).toBeTruthy();

    fireEvent.click(screen.getByText(named('feature/login')));
    expect(pressed(name)).toBe('false');
    expect(screen.queryByText('Behind the remote by 3')).toBeNull();
  });

  it('shows a trunk right under its title in the graph', async () => {
    const { host } = sidebarHost({ repos: ['/demo/repo'] });
    const { container } = render(<App host={host} />);
    await screen.findByText(named('feature/login'));
    fireEvent.click(screen.getByRole('button', { name: 'Graph' }));
    const title = screen.getByRole('button', { name: 'Trunk main' });
    fireEvent.click(title);
    expect(pressed(title)).toBe('true');
    const after = title.nextElementSibling!;
    expect(after.classList.contains('g-inline')).toBe(true);
    expect(after.textContent).toContain('Compared through');
    expect(container.querySelectorAll('.g-inline')).toHaveLength(1);
  });
});

describe('narrow layout rules', () => {
  const css = fs.readFileSync(path.resolve(__dirname, '../app.css'), 'utf8');
  const rule = (selector: string) => {
    const at = css.indexOf(`\n${selector} {`);
    expect(at, selector).toBeGreaterThanOrEqual(0);
    return css.slice(at, css.indexOf('}', at));
  };

  it('shows the status chips in the details of an open branch; only compact rows hide chips', async () => {
    const style = document.createElement('style');
    style.textContent = css;
    document.head.appendChild(style);
    try {
      const { host } = sidebarHost({ repos: ['/demo/repo'] });
      const { container } = render(<App host={host} />);
      fireEvent.click(await screen.findByText(named('feature/search')));
      const chips = [...container.querySelectorAll('.list.compact .detail .chip')];
      expect(chips.length).toBeGreaterThan(0);
      for (const c of chips) expect(getComputedStyle(c).display, c.textContent!).not.toBe('none');
      // a chip in a row of a compact list stays hidden: the rows show names only
      const list = document.createElement('div');
      list.className = 'list compact';
      list.innerHTML = '<button class="row"><span class="chip">x</span></button>';
      document.body.appendChild(list);
      expect(getComputedStyle(list.querySelector('.chip')!).display).toBe('none');
      list.remove();
    } finally {
      style.remove();
    }
  });

  it('never wraps the text of a toggle button', () => {
    expect(rule('.seg .btn')).toMatch(/white-space:\s*nowrap/);
  });

  it('puts the dialog backdrop above every other layer, the sticky sidebar header included', () => {
    const overlay = Number(/z-index:\s*(\d+)/.exec(rule('.overlay'))?.[1] ?? NaN);
    expect(overlay).toBeGreaterThan(0);
    // every other z-index in the stylesheet is below it
    const others = [...css.matchAll(/([^{}]+)\{[^}]*?z-index:\s*(\d+)/g)]
      .map((m) => ({ selector: m[1].trim().split('\n').pop()!.trim(), z: Number(m[2]) }))
      .filter((o) => o.selector !== '.overlay');
    expect(others.map((o) => o.selector)).toContain('.side-head');
    for (const o of others) expect(o.z, o.selector).toBeLessThan(overlay);
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
