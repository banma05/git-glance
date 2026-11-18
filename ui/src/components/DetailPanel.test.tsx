import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/preact';
import { DetailPanel } from './DetailPanel';
import { App } from './App';
import { branchOf } from './graphFixture';
import { makeT } from '../i18n';
import { blockText } from '../format';
import { mockView } from '../host/mock';
import { HostError } from '../host/types';
import type { Host } from '../host/types';
import type { Actions, AppState } from '../state';
import type { BranchView, OpResult } from '../types';

const zh = makeT('zh-cn');
const en = makeT('en');

function actions(): Actions {
  const fns: Record<string, unknown> = {};
  return new Proxy({} as Actions, {
    get: (_t, k: string) => (fns[k] ??= vi.fn()),
  });
}

type Extra = Pick<AppState, 'busy' | 'switchResult'>;
const idle: Extra = { busy: false, switchResult: null };

function renderPanel(branch: BranchView | null, state: Extra = idle, t = zh, a: Actions = actions()) {
  return render(<DetailPanel t={t} branch={branch} state={state} actions={a} />);
}

afterEach(cleanup);

describe('DetailPanel', () => {
  it('disables the remove button and gives the reason when the folder cannot be removed', () => {
    renderPanel(branchOf('x', [], { folder: '/w/wt-x', canRemoveFolder: false, removeBlock: 'dirty' }));
    const btn = screen.getByRole('button', { name: /删除文件夹/ }) as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
    expect(btn.textContent).toContain(blockText('dirty', zh));
  });

  it('opens the remove dialog instead of removing straight away', () => {
    const a = actions();
    renderPanel(branchOf('x', [], { folder: '/w/wt-x', canRemoveFolder: true, removeBlock: '' }), idle, zh, a);
    fireEvent.click(screen.getByRole('button', { name: '删除文件夹' }));
    expect(a.openDialog).toHaveBeenCalledWith('remove');
  });

  it('has no open button but a switch and a new folder button when the branch has no folder', () => {
    renderPanel(branchOf('x', [], { folder: '' }));
    expect(screen.queryByRole('button', { name: '在编辑器打开' })).toBeNull();
    expect(screen.getByRole('button', { name: '切换到这里' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '新建文件夹' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '复制分支名' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '和…对比' })).toBeTruthy();
  });

  it('has an open button and no switch button when the branch has a folder', () => {
    const a = actions();
    renderPanel(branchOf('x', [], { folder: '/w/wt-x' }), idle, zh, a);
    expect(screen.queryByRole('button', { name: '切换到这里' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: '在编辑器打开' }));
    expect(a.openFolder).toHaveBeenCalledWith('/w/wt-x');
  });

  it('copies the branch name', () => {
    const a = actions();
    renderPanel(branchOf('alice/x', []), idle, zh, a);
    fireEvent.click(screen.getByRole('button', { name: '复制分支名' }));
    expect(a.copy).toHaveBeenCalledWith('alice/x');
  });

  it('shows the switch failure reason and the returned path', () => {
    const result: OpResult = { ok: false, reason: 'checked-out-elsewhere', files: [], path: '/x', logError: '' };
    renderPanel(branchOf('x', []), { busy: false, switchResult: { branch: 'x', result } });
    expect(screen.getByText(/这个分支已经放在别的文件夹里了/)).toBeTruthy();
    expect(screen.getByText(/\/x/)).toBeTruthy();
  });

  it('shows the log error of a switch', () => {
    const result: OpResult = { ok: true, reason: '', files: [], path: '', logError: 'disk full' };
    renderPanel(branchOf('x', []), { busy: false, switchResult: { branch: 'x', result } }, en);
    expect(screen.getByText(/disk full/)).toBeTruthy();
  });

  it('does not show the result of another branch', () => {
    const result: OpResult = { ok: false, reason: 'dirty', files: [], path: '', logError: '' };
    renderPanel(branchOf('x', []), { busy: false, switchResult: { branch: 'other', result } }, en);
    expect(screen.queryByText(/uncommitted/)).toBeNull();
  });

  it('notes where the trunk came from unless it was counted', () => {
    renderPanel(branchOf('x', [], { trunkSource: 'created' }));
    expect(screen.getByText(/按新建时的起点/)).toBeTruthy();
    cleanup();
    renderPanel(branchOf('x', [], { trunkSource: 'manual' }));
    expect(screen.getByText(/按你的指定/)).toBeTruthy();
    cleanup();
    renderPanel(branchOf('x', [], { trunkSource: 'count' }));
    expect(screen.queryByText(/按新建时的起点|按你的指定/)).toBeNull();
  });

  it('lists the changed files, the matching step per commit and the release state', () => {
    const c = { hash: 'abcdef1234', subject: 'Add form', time: 1 };
    renderPanel(
      branchOf('x', [c], {
        folder: '/w/wt-x',
        dirty: { changed: 60, untracked: 0, files: ['a.ts', 'b.ts'] },
        test: { state: 'all', matched: 1, total: 1, per: [{ ...c, by: 'patch' }] },
        release: { state: 'some', in: ['main'], notIn: ['release/1'] },
      }),
      idle,
      en,
    );
    expect(screen.getByText('a.ts')).toBeTruthy();
    expect(screen.getByText(/58 more not listed/)).toBeTruthy();
    expect(screen.getByText(/Add form/)).toBeTruthy();
    expect(screen.getByText(/same change/)).toBeTruthy();
    expect(screen.getByText(/In: main/)).toBeTruthy();
    expect(screen.getByText(/Not in: release\/1/)).toBeTruthy();
  });

  it('has no push, pull, sync, merge or rebase button', () => {
    for (const t of [zh, en]) {
      renderPanel(branchOf('x', [], { folder: '' }), idle, t);
      for (const b of screen.getAllByRole('button')) {
        expect(b.textContent ?? '').not.toMatch(/push|pull|sync|merge|rebase|推送|拉取|同步|合并|变基/i);
      }
      cleanup();
    }
  });
});

describe('DetailPanel in the app', () => {
  function appHost(switchResult: () => Promise<OpResult>) {
    const calls: Array<{ method: string; params: unknown }> = [];
    const host: Host = {
      async call<T>(method: string, params?: object): Promise<T> {
        calls.push({ method, params });
        switch (method) {
          case 'repos':
            return ['/demo/repo'] as T;
          case 'view':
            return mockView('/demo/repo') as T;
          case 'fetch':
            return { ok: true, attempts: 1, error: '', at: 1 } as T;
          case 'getGlobal':
            return { repos: ['/demo/repo'], editor: 'code', theme: 'auto' } as T;
          case 'switch':
            return (await switchResult()) as T;
          default:
            throw new HostError('unknown-method', method);
        }
      },
      openFolder: async () => undefined,
      copy: async () => undefined,
      openDiff: async () => false,
      openWide: () => undefined,
      mode: 'wide',
      locale: 'zh-cn',
      theme: () => 'dark',
      onTheme: () => undefined,
    };
    return { host, calls };
  }

  const switchBtn = () => screen.getByRole('button', { name: '切换到这里' }) as HTMLButtonElement;

  it('disables the switch button while the call is pending and shows the result after', async () => {
    let finish: (r: OpResult) => void = () => undefined;
    const { host, calls } = appHost(() => new Promise<OpResult>((r) => (finish = r)));
    render(<App host={host} />);
    fireEvent.click(await screen.findByText('feature/search'));
    fireEvent.click(await screen.findByRole('button', { name: '切换到这里' }));
    await waitFor(() => expect(switchBtn().disabled).toBe(true));
    fireEvent.click(switchBtn());
    expect(calls.filter((c) => c.method === 'switch')).toHaveLength(1);
    expect(calls.find((c) => c.method === 'switch')!.params).toMatchObject({
      repo: '/demo/repo',
      folder: '/demo/repo',
      branch: 'carol/feature/search',
    });
    await waitFor(() => expect(calls.filter((c) => c.method === 'view')).toHaveLength(2));
    finish({ ok: false, reason: 'checked-out-elsewhere', files: [], path: '/x', logError: '' });
    expect(await screen.findByText(/这个分支已经放在别的文件夹里了/)).toBeTruthy();
    expect(screen.getByText(/\/x/)).toBeTruthy();
    await waitFor(() => expect(switchBtn().disabled).toBe(false));
    expect(calls.filter((c) => c.method === 'view')).toHaveLength(2); // a refusal does not reload
  });

  it('reloads the view after a successful switch', async () => {
    const { host, calls } = appHost(async () => ({ ok: true, reason: '', files: [], path: '', logError: '' }));
    render(<App host={host} />);
    fireEvent.click(await screen.findByText('feature/search'));
    await waitFor(() => expect(calls.filter((c) => c.method === 'view')).toHaveLength(2));
    fireEvent.click(await screen.findByRole('button', { name: '切换到这里' }));
    await waitFor(() => expect(calls.filter((c) => c.method === 'view')).toHaveLength(3));
  });
});
