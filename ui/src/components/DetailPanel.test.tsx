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
import type { BranchView, OpResult, SwitchResult } from '../types';

// named finds a branch name, which BranchName writes in two parts.
const named = (n: string) => (_: string, el: Element | null) =>
  !!el?.classList.contains('bname') &&
  (el.querySelector('.bname-pre')?.textContent ?? '') + el.querySelector('.bname-last')!.textContent === n;


const zh = makeT('zh-cn');
const en = makeT('en');

function actions(): Actions {
  const fns: Record<string, unknown> = {};
  return new Proxy({} as Actions, {
    get: (_t, k: string) => (fns[k] ??= vi.fn()),
  });
}

type Extra = Pick<AppState, 'busy' | 'switchResult'> & Partial<Pick<AppState, 'news'>>;
// sw is a switch answer with nothing shelved or put back
const sw = (r: OpResult): SwitchResult => ({ shelved: null, restored: null, keptShelf: null, fastForwarded: false, aheadNote: '', ...r });
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

  it('opens the folder dialog for this existing branch', () => {
    const a = actions();
    renderPanel(branchOf('x', [], { folder: '' }), idle, en, a);
    fireEvent.click(screen.getByRole('button', { name: en('detail.create') }));
    expect(a.openDialog).toHaveBeenCalledWith('createExisting');
  });

  it('assigns the branch to a trunk only on apply, and can go back to automatic', () => {
    const a = actions();
    const trunks = { trunks: ['main', 'v2'], current: '' };
    render(<DetailPanel t={en} branch={branchOf('x', [], { trunk: '', trunkSource: 'unknown' })} state={idle} actions={a} assign={trunks} />);
    const pick = screen.getByLabelText(en('detail.assign')) as HTMLSelectElement;
    expect([...pick.options].map((o) => o.value)).toEqual(['', 'main', 'v2']);
    expect(pick.value).toBe('');
    const apply = screen.getByRole('button', { name: en('detail.assignApply') }) as HTMLButtonElement;
    expect(apply.disabled).toBe(true); // nothing changed yet
    fireEvent.change(pick, { target: { value: 'v2' } });
    expect(a.assign).not.toHaveBeenCalled();
    fireEvent.click(apply);
    expect(a.assign).toHaveBeenCalledWith('x', 'v2');
    cleanup();
    const b = actions();
    render(<DetailPanel t={en} branch={branchOf('x', [])} state={idle} actions={b} assign={{ trunks: ['main', 'v2'], current: 'main' }} />);
    fireEvent.change(screen.getByLabelText(en('detail.assign')), { target: { value: '' } });
    fireEvent.click(screen.getByRole('button', { name: en('detail.assignApply') }));
    expect(b.assign).toHaveBeenCalledWith('x', '');
  });

  it('cannot assign while a change runs or when the core refuses changes', () => {
    const a = actions();
    const props = { t: en, branch: branchOf('x', []), actions: a, assign: { trunks: ['main', 'v2'], current: '' } };
    render(<DetailPanel {...props} state={{ busy: false, switchResult: null, gitProblem: { kind: 'old', version: '2.29' } }} />);
    fireEvent.change(screen.getByLabelText(en('detail.assign')), { target: { value: 'v2' } });
    expect((screen.getByRole('button', { name: en('detail.assignApply') }) as HTMLButtonElement).disabled).toBe(true);
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
    const result: OpResult = { ok: false, reason: 'checked-out-elsewhere', files: [], path: '/x', detail: '', logError: '' };
    renderPanel(branchOf('x', []), { busy: false, switchResult: { branch: 'x', result: sw(result) } });
    expect(screen.getByText(/这个分支已经放在别的文件夹里了/)).toBeTruthy();
    expect(screen.getByText(/\/x/)).toBeTruthy();
  });

  it('shows the log error of a switch', () => {
    const result: OpResult = { ok: true, reason: '', files: [], path: '', detail: '', logError: 'disk full' };
    renderPanel(branchOf('x', []), { busy: false, switchResult: { branch: 'x', result: sw(result) } }, en);
    expect(screen.getByText(/disk full/)).toBeTruthy();
  });

  it('shows what git said when a switch failed', () => {
    const result: OpResult = { ok: false, reason: 'git-failed', files: [], path: '/w', detail: 'error: would be overwritten', logError: '' };
    renderPanel(branchOf('x', []), { busy: false, switchResult: { branch: 'x', result: sw(result) } }, en);
    expect(screen.getByText(en('switch.reason.git-failed'))).toBeTruthy();
    expect(screen.getByText(/would be overwritten/)).toBeTruthy();
  });

  it('says when the details of the branch could not be read', () => {
    renderPanel(branchOf('x', [], { error: 'fatal: bad object 1234', trunk: '', trunkSource: 'unknown' }), idle, en);
    expect(screen.getByText(en('detail.error', { message: 'fatal: bad object 1234' }))).toBeTruthy();
  });

  it('does not show the result of another branch', () => {
    const result: OpResult = { ok: false, reason: 'dirty', files: [], path: '', detail: '', logError: '' };
    renderPanel(branchOf('x', []), { busy: false, switchResult: { branch: 'other', result: sw(result) } }, en);
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
    const c = { hash: 'abcdef1234', subject: 'Add form', time: 1, author: 'alice' };
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

  it('offers the trunk news of a branch that has a trunk, with how many trunk commits it lacks', () => {
    const a = actions();
    renderPanel(branchOf('x', [], { trunk: 'main', behindTrunk: 3 }), idle, en, a);
    const news = screen.getByRole('button', { name: 'Trunk news (3)' });
    expect(news.getAttribute('aria-pressed')).toBe('false');
    fireEvent.click(news);
    expect(a.toggleNews).toHaveBeenCalledWith({ kind: 'branch', branch: 'x' });
    cleanup();
    renderPanel(branchOf('x', [], { trunk: 'main', behindTrunk: 200 }), idle, en);
    expect(screen.getByRole('button', { name: 'Trunk news (200)' })).toBeTruthy();
    cleanup();
    renderPanel(branchOf('x', [], { trunk: 'main', behindTrunk: 205 }), idle, zh);
    expect(screen.getByRole('button', { name: zh('news.open', { n: '200+' }) })).toBeTruthy();
    cleanup();
    renderPanel(branchOf('x', [], { trunk: '' }), idle, en);
    expect(screen.queryByRole('button', { name: /Trunk news/ })).toBeNull();
  });

  it('shows the trunk news button pressed while the news of this branch is open', () => {
    renderPanel(branchOf('x', [], { trunk: 'main' }), { ...idle, news: { kind: 'branch', branch: 'x' } }, en);
    expect(screen.getByRole('button', { name: 'Trunk news (0)' }).getAttribute('aria-pressed')).toBe('true');
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
    fireEvent.click(await screen.findByText(named('feature/search')));
    fireEvent.click(await screen.findByRole('button', { name: '切换到这里' }));
    await waitFor(() => expect(switchBtn().disabled).toBe(true));
    fireEvent.click(switchBtn());
    // the remote is read first, then the folder is switched once
    await waitFor(() => expect(calls.filter((c) => c.method === 'switch')).toHaveLength(1));
    expect(calls.filter((c) => c.method === 'fetch')).toHaveLength(2);
    expect(calls.find((c) => c.method === 'switch')!.params).toMatchObject({
      repo: '/demo/repo',
      folder: '/demo/repo',
      branch: 'carol/feature/search',
    });
    await waitFor(() => expect(calls.filter((c) => c.method === 'view')).toHaveLength(2));
    finish({ ok: false, reason: 'checked-out-elsewhere', files: [], path: '/x', detail: '', logError: '' });
    expect(await screen.findByText(/这个分支已经放在别的文件夹里了/)).toBeTruthy();
    expect(screen.getByText(/\/x/)).toBeTruthy();
    await waitFor(() => expect(switchBtn().disabled).toBe(false));
    // the refusal changed nothing, but the remote was just read: the view is read once more
    await waitFor(() => expect(calls.filter((c) => c.method === 'view')).toHaveLength(3));
  });

  it('reloads the view after a successful switch', async () => {
    const { host, calls } = appHost(async () => ({ ok: true, reason: '', files: [], path: '', detail: '', logError: '' }));
    render(<App host={host} />);
    fireEvent.click(await screen.findByText(named('feature/search')));
    await waitFor(() => expect(calls.filter((c) => c.method === 'view')).toHaveLength(2));
    fireEvent.click(await screen.findByRole('button', { name: '切换到这里' }));
    await waitFor(() => expect(calls.filter((c) => c.method === 'view')).toHaveLength(3));
    expect(calls.filter((c) => c.method === 'switch')[0].params).toEqual({
      repo: '/demo/repo',
      folder: '/demo/repo',
      branch: 'carol/feature/search',
      latest: true,
      shelve: false,
      remote: '',
    });
  });
});
