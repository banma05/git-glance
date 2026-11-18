import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/preact';
import { useEffect, useMemo, useRef } from 'preact/hooks';
import { Dialogs } from './Dialogs';
import { RepoTabs } from '../RepoTabs';
import { NoticeLine } from '../NoticeLine';
import { makeT } from '../../i18n';
import { mockView } from '../../host/mock';
import { blockText } from '../../format';
import { HostError } from '../../host/types';
import type { Host } from '../../host/types';
import { useApp } from '../../state';
import type { DialogName } from '../../state';

const FORBIDDEN = /push|pull|sync|merge|rebase|推送|拉取|同步|合并|变基/i;
const REPO = '/demo/repo';
const hex = (n: number) => n.toString(16).padStart(40, '0');

type Handler = (params: Record<string, unknown>) => unknown;

function fakeHost(over: Record<string, unknown> = {}, locale = 'en') {
  const calls: Array<{ method: string; params: Record<string, unknown> }> = [];
  const openDiff = vi.fn(async (..._args: unknown[]) => false);
  const host: Host = {
    async call<T>(method: string, params?: object): Promise<T> {
      const p = (params ?? {}) as Record<string, unknown>;
      calls.push({ method, params: p });
      const v = over[method];
      if (v instanceof Error) throw v;
      if (v !== undefined) return (typeof v === 'function' ? await (v as Handler)(p) : v) as T;
      switch (method) {
        case 'repos':
          return [REPO] as T;
        case 'view':
          return mockView(REPO) as T;
        case 'fetch':
          return { ok: true, attempts: 1, error: '', at: 1 } as T;
        case 'getGlobal':
          return { repos: [REPO], editor: 'code', theme: 'auto' } as T;
        case 'backups':
          return { dir: '/cfg/backups', bytes: 2048 } as T;
        case 'defaultFolder':
          return { path: `${REPO}-${String(p.branch ?? '').replace(/\//g, '-')}` } as T;
        case 'ops':
          return [] as T;
        case 'remoteBranches':
          return [] as T;
        default:
          throw new HostError('unknown-method', method);
      }
    },
    openFolder: async () => undefined,
    copy: async () => undefined,
    openDiff: (repo, a, b, path) => openDiff(repo, a, b, path),
    openWide: () => undefined,
    mode: 'wide',
    locale,
    theme: () => 'dark',
    onTheme: () => undefined,
  };
  return { host, calls, openDiff, count: (m: string) => calls.filter((c) => c.method === m).length };
}

function Harness({ host, open, select }: { host: Host; open: DialogName; select?: string }) {
  const { state, actions } = useApp(host);
  const t = useMemo(() => makeT(host.locale), [host]);
  const done = useRef(false);
  useEffect(() => {
    if (done.current || !state.view) return;
    done.current = true;
    if (select) actions.select(select);
    if (open === 'align') actions.align();
    else actions.openDialog(open);
  }, [state.view]);
  return (
    <>
      {state.notice && <NoticeLine t={t} notice={state.notice} onDismiss={actions.dismissNotice} />}
      <Dialogs host={host} t={t} state={state} actions={actions} />
    </>
  );
}

const en = makeT('en');
const typo = mockView(REPO).branches['alice/fix/typo'];
const removeOk = {
  ok: true,
  reason: '',
  files: [],
  backupDir: '/cfg/backups/typo-1',
  backupBytes: 1536,
  deletedBuild: ['node_modules', 'target'],
  skippedLinks: ['linked-dir'],
  detail: 'extra detail text',
  logError: 'disk is full',
};

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe('RemoveFolderConfirm', () => {
  const open = (over: Record<string, unknown> = {}) => {
    const h = fakeHost({ removeFolder: removeOk, ...over });
    render(<Harness host={h.host} open="remove" select="alice/fix/typo" />);
    return h;
  };

  it('states the rules and calls removeFolder only after the confirm click', async () => {
    const h = open();
    const confirm = await screen.findByRole('button', { name: en('dialog.remove.confirm') });
    expect(screen.getByRole('dialog').textContent).toContain('alice/fix/typo');
    expect(screen.getByRole('dialog').textContent).toContain('/demo/repo-typo');
    await waitFor(() => expect(screen.getByRole('dialog').textContent).toContain('/cfg/backups'));
    expect(screen.getByRole('dialog').textContent).toContain('node_modules');
    expect(h.count('removeFolder')).toBe(0);
    fireEvent.click(confirm);
    await waitFor(() => expect(h.count('removeFolder')).toBe(1));
    expect(h.calls.find((c) => c.method === 'removeFolder')?.params).toEqual({
      repo: REPO,
      branch: 'alice/fix/typo',
      folder: '/demo/repo-typo',
      head: typo.folderHead,
    });
  });

  it('never confirms another HEAD than it showed: a view read since with a new HEAD disables it', async () => {
    let views = 0;
    const later = mockView(REPO);
    later.branches['alice/fix/typo'] = { ...later.branches['alice/fix/typo'], folderHead: hex(77) };
    const h = open({ view: () => (++views === 1 ? mockView(REPO) : later) });
    const confirm = (await screen.findByRole('button', { name: en('dialog.remove.confirm') })) as HTMLButtonElement;
    expect(screen.getByRole('dialog').textContent).toContain('/demo/repo-typo');
    await waitFor(() => expect(confirm.disabled).toBe(true));
    expect(screen.getByRole('dialog').textContent).toContain(en('dialog.remove.reopen'));
    fireEvent.click(confirm);
    await new Promise((r) => setTimeout(r, 20));
    expect(h.count('removeFolder')).toBe(0);
  });

  it('keeps the confirm disabled and names the reason when the folder cannot be removed', async () => {
    const h = fakeHost({ removeFolder: removeOk });
    render(<Harness host={h.host} open="remove" select="alice/feature/login" />);
    const confirm = (await screen.findByRole('button', { name: en('dialog.remove.confirm') })) as HTMLButtonElement;
    expect(confirm.disabled).toBe(true);
    expect(screen.getByRole('dialog').textContent).toContain(blockText('dirty', en));
    fireEvent.click(confirm);
    expect(h.count('removeFolder')).toBe(0);
  });

  it('disables the confirm when the view read meanwhile says the folder can no longer be removed', async () => {
    let views = 0;
    const later = mockView(REPO);
    later.branches['alice/fix/typo'] = { ...later.branches['alice/fix/typo'], canRemoveFolder: false, removeBlock: 'dirty' };
    const h = open({ view: () => (++views === 1 ? mockView(REPO) : later) });
    const confirm = (await screen.findByRole('button', { name: en('dialog.remove.confirm') })) as HTMLButtonElement;
    await waitFor(() => expect(confirm.disabled).toBe(true));
    expect(screen.getByRole('dialog').textContent).toContain(blockText('dirty', en));
    expect(h.count('removeFolder')).toBe(0);
  });

  it('states what is checked right before the removal', async () => {
    open();
    await screen.findByRole('button', { name: en('dialog.remove.confirm') });
    expect(screen.getByRole('dialog').textContent).toContain(en('dialog.remove.rule.changed'));
    expect(en('dialog.remove.rule.changed')).toMatch(/checked again/);
    expect(en('dialog.remove.rule.changed')).not.toMatch(/since the view was drawn/);
  });

  it('shows the backup, deleted build folders, skipped links, detail and the log error', async () => {
    const h = open();
    fireEvent.click(await screen.findByRole('button', { name: en('dialog.remove.confirm') }));
    const d = screen.getByRole('dialog');
    await waitFor(() => expect(d.textContent).toContain('/cfg/backups/typo-1'));
    expect(d.textContent).toContain('1.5 KB');
    expect(d.textContent).toContain('node_modules, target');
    expect(d.textContent).toContain('linked-dir');
    expect(d.textContent).toContain('extra detail text');
    expect(d.textContent).toContain('disk is full');
    await waitFor(() => expect(h.count('view')).toBeGreaterThanOrEqual(3)); // the view is read again
  });

  it('shows the reason text of a refusal', async () => {
    open({ removeFolder: { ...removeOk, ok: false, reason: 'locked', backupDir: '', deletedBuild: [], skippedLinks: [], detail: '', logError: '' } });
    fireEvent.click(await screen.findByRole('button', { name: en('dialog.remove.confirm') }));
    await waitFor(() => expect(screen.getByRole('dialog').textContent).toContain(en('remove.reason.locked')));
  });

  it('lists the links already removed when removing links failed partway', async () => {
    open({ removeFolder: { ...removeOk, ok: false, reason: 'links-failed', backupDir: '', deletedBuild: [], skippedLinks: ['node_modules/dep'], detail: 'access denied', logError: '' } });
    fireEvent.click(await screen.findByRole('button', { name: en('dialog.remove.confirm') }));
    const d = screen.getByRole('dialog');
    await waitFor(() => expect(d.textContent).toContain(en('remove.reason.links-failed')));
    expect(d.textContent).toContain('node_modules/dep');
    expect(d.textContent).toContain('access denied');
  });

  it('names links removed before a failure as removed (link only), and the others as skipped', async () => {
    open({
      removeFolder: { ...removeOk, ok: false, reason: 'links-failed', backupDir: '', deletedBuild: [], skippedLinks: ['notes/link'], removedLinks: ['build/dep'], detail: '', logError: '' },
    });
    fireEvent.click(await screen.findByRole('button', { name: en('dialog.remove.confirm') }));
    const d = screen.getByRole('dialog');
    await waitFor(() => expect(d.textContent).toContain(en('dialog.remove.removedLinks', { list: 'build/dep' })));
    expect(d.textContent).toContain(en('dialog.remove.linksAfterLinks', { list: 'notes/link' }));
  });

  it('says the skipped links of a refusal were not touched', async () => {
    open({ removeFolder: { ...removeOk, ok: false, reason: 'locked', backupDir: '', deletedBuild: [], skippedLinks: ['notes/link'], detail: '', logError: '' } });
    fireEvent.click(await screen.findByRole('button', { name: en('dialog.remove.confirm') }));
    await waitFor(() => expect(screen.getByRole('dialog').textContent).toContain(en('dialog.remove.linksRefused', { list: 'notes/link' })));
    expect(en('dialog.remove.linksRefused', { list: 'x' })).toMatch(/nothing in the folder was touched/);
  });

  // what a refusal says about the links depends on whether anything ran
  const refusal = (reason: string) =>
    open({
      removeFolder: { ...removeOk, ok: false, reason, backupDir: '', deletedBuild: [], skippedLinks: ['notes/link'], removedLinks: ['build/dep'], detail: '', logError: '' },
    });
  const touched = /nothing in the folder was touched|文件夹里什么都没有动/;

  it('after git started removing the folder, does not claim nothing was touched', async () => {
    refusal('git-failed');
    fireEvent.click(await screen.findByRole('button', { name: en('dialog.remove.confirm') }));
    const d = screen.getByRole('dialog');
    await waitFor(() => expect(d.textContent).toContain(en('dialog.remove.linksAfterGit', { list: 'notes/link' })));
    expect(d.textContent).toContain(en('dialog.remove.removedLinks', { list: 'build/dep' }));
    expect(d.textContent).not.toMatch(touched);
  });

  it('after removing links failed partway, says only the removed links are gone', async () => {
    refusal('links-failed');
    fireEvent.click(await screen.findByRole('button', { name: en('dialog.remove.confirm') }));
    const d = screen.getByRole('dialog');
    await waitFor(() => expect(d.textContent).toContain(en('dialog.remove.linksAfterLinks', { list: 'notes/link' })));
    expect(d.textContent).toContain(en('dialog.remove.removedLinks', { list: 'build/dep' }));
    expect(d.textContent).not.toMatch(touched);
  });

  it('for a reason it does not know, only says the links were not followed', async () => {
    refusal('from-the-future');
    fireEvent.click(await screen.findByRole('button', { name: en('dialog.remove.confirm') }));
    const d = screen.getByRole('dialog');
    await waitFor(() => expect(d.textContent).toContain(en('dialog.remove.linksNotFollowed', { list: 'notes/link' })));
    expect(d.textContent).not.toMatch(touched);
  });

  it('sends one call on a double click', async () => {
    let release: (v: unknown) => void = () => undefined;
    const slow = () => new Promise((r) => (release = r));
    const h = open({ removeFolder: slow });
    const confirm = await screen.findByRole('button', { name: en('dialog.remove.confirm') });
    fireEvent.click(confirm);
    fireEvent.click(confirm);
    await waitFor(() => expect(h.count('removeFolder')).toBe(1));
    expect((confirm as HTMLButtonElement).disabled).toBe(true);
    await act(async () => release(removeOk));
    expect(h.count('removeFolder')).toBe(1);
  });

  it('closes with Esc and keeps the result of a write that is still running', async () => {
    let release: (v: unknown) => void = () => undefined;
    const h = open({ removeFolder: () => new Promise((r) => (release = r)) });
    fireEvent.click(await screen.findByRole('button', { name: en('dialog.remove.confirm') }));
    await waitFor(() => expect(h.count('removeFolder')).toBe(1));
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('dialog')).toBeNull();
    await act(async () => release(removeOk));
    await waitFor(() => expect(h.count('view')).toBeGreaterThanOrEqual(3));
  });
});

describe('Settings', () => {
  it('asks for a confirm before clearBackups and says the backups may be the only copy', async () => {
    const h = fakeHost({ clearBackups: {} });
    render(<Harness host={h.host} open="settings" />);
    const clear = await screen.findByRole('button', { name: en('dialog.settings.clearBackups') });
    await waitFor(() => expect(screen.getByRole('dialog').textContent).toContain('2 KB'));
    fireEvent.click(clear);
    expect(h.count('clearBackups')).toBe(0);
    expect(screen.getByRole('dialog').textContent).toMatch(/only copy/);
    fireEvent.click(screen.getByRole('button', { name: en('dialog.settings.clearYes') }));
    await waitFor(() => expect(h.count('clearBackups')).toBe(1));
  });

  it('does not call clearBackups when the confirm is cancelled', async () => {
    const h = fakeHost({ clearBackups: {} });
    render(<Harness host={h.host} open="settings" />);
    fireEvent.click(await screen.findByRole('button', { name: en('dialog.settings.clearBackups') }));
    fireEvent.click(screen.getByRole('button', { name: en('app.cancel') }));
    expect(h.count('clearBackups')).toBe(0);
  });

  it('sets a role, searching remote branches', async () => {
    const h = fakeHost({
      remoteBranches: ['release/2.0'],
      setRole: () => mockView(REPO).settings,
    });
    render(<Harness host={h.host} open="settings" />);
    const input = (await screen.findByLabelText(en('dialog.settings.branch'))) as HTMLInputElement;
    fireEvent.input(input, { target: { value: 'release' } });
    fireEvent.click(screen.getByRole('button', { name: en('dialog.settings.searchRemote') }));
    await waitFor(() => expect(h.calls.find((c) => c.method === 'remoteBranches')?.params).toEqual({ repo: REPO, query: 'release' }));
    await waitFor(() => expect(document.querySelector('option[value="release/2.0"]')).toBeTruthy());
    fireEvent.input(input, { target: { value: 'release/2.0' } });
    fireEvent.change(screen.getByLabelText(en('dialog.settings.role')), { target: { value: 'release' } });
    fireEvent.change(screen.getByLabelText(en('dialog.settings.trunkOf')), { target: { value: 'develop' } });
    fireEvent.click(screen.getByRole('button', { name: en('dialog.settings.apply') }));
    await waitFor(() => expect(h.count('setRole')).toBe(1));
    expect(h.calls.find((c) => c.method === 'setRole')?.params).toEqual({ repo: REPO, branch: 'release/2.0', role: 'release', trunk: 'develop' });
  });

  it('disables every setting that is saved by the core when git is too old', async () => {
    const h = fakeHost({ version: { core: 'x', git: '2.29', gitOK: false } });
    render(<Harness host={h.host} open="settings" />);
    await waitFor(() => expect((screen.getByLabelText(en('dialog.settings.theme')) as HTMLSelectElement).disabled).toBe(true));
    expect((screen.getByLabelText(en('toolbar.openWith')) as HTMLSelectElement).disabled).toBe(true);
    expect((screen.getByRole('button', { name: en('dialog.settings.clearBackups') }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole('button', { name: en('dialog.settings.save') }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole('button', { name: en('dialog.settings.searchRemote') }) as HTMLButtonElement).disabled).toBe(false);
  });

  it('saves the hidden prefix and old-branch days, and the editor and theme', async () => {
    const h = fakeHost({ setRepoSettings: () => mockView(REPO).settings, setGlobal: { repos: [], editor: 'cursor', theme: 'dark' } });
    render(<Harness host={h.host} open="settings" />);
    fireEvent.input(await screen.findByLabelText(en('dialog.settings.hidePrefix')), { target: { value: 'bot/' } });
    fireEvent.input(screen.getByLabelText(en('dialog.settings.oldDays')), { target: { value: '45' } });
    fireEvent.click(screen.getByRole('button', { name: en('dialog.settings.save') }));
    await waitFor(() => expect(h.count('setRepoSettings')).toBe(1));
    expect(h.calls.find((c) => c.method === 'setRepoSettings')?.params).toEqual({ repo: REPO, hidePrefix: 'bot/', oldDays: 45 });
    fireEvent.change(screen.getByLabelText(en('toolbar.openWith')), { target: { value: 'cursor' } });
    await waitFor(() => expect(h.calls.find((c) => c.method === 'setGlobal')?.params).toEqual({ editor: 'cursor' }));
    fireEvent.change(screen.getByLabelText(en('dialog.settings.theme')), { target: { value: 'dark' } });
    await waitFor(() => expect(h.calls.filter((c) => c.method === 'setGlobal').length).toBe(2));
  });
});

describe('Compare', () => {
  const result = {
    onlyA: [{ hash: hex(1), subject: 'Only on A', time: 1 }],
    onlyB: [{ hash: hex(2), subject: 'Only on B', time: 1 }],
    files: [{ path: 'src/a.ts', status: 'M' }],
    filesTruncated: true,
  };
  const run = async (over: Record<string, unknown> = {}) => {
    const h = fakeHost({ compare: result, ...over });
    render(<Harness host={h.host} open="compare" select="alice/fix/typo" />);
    const a = (await screen.findByLabelText(en('dialog.compare.a'))) as HTMLSelectElement;
    expect(a.value).toBe('alice/fix/typo');
    fireEvent.change(screen.getByLabelText(en('dialog.compare.b')), { target: { value: 'bob/fix/timeout' } });
    fireEvent.click(screen.getByRole('button', { name: en('dialog.compare.run') }));
    return h;
  };

  it('shows both sides and the cut note, and falls back to fileDiff when openDiff returns false', async () => {
    const h = await run({ fileDiff: { diff: '@@ -1 +1 @@\n-old\n+new', truncated: true } });
    await screen.findByText('Only on A');
    expect(screen.getByText('Only on B')).toBeTruthy();
    expect(screen.getByText(en('dialog.compare.filesTruncated'))).toBeTruthy();
    expect(h.calls.find((c) => c.method === 'compare')?.params).toEqual({
      repo: REPO,
      a: 'refs/heads/alice/fix/typo',
      b: 'refs/heads/bob/fix/timeout',
    });
    fireEvent.click(screen.getByText('src/a.ts'));
    await waitFor(() =>
      expect(h.openDiff).toHaveBeenCalledWith(REPO, 'refs/heads/alice/fix/typo', 'refs/heads/bob/fix/timeout', 'src/a.ts'),
    );
    await waitFor(() => expect(h.count('fileDiff')).toBe(1));
    await waitFor(() => expect(screen.getByRole('dialog').textContent).toContain('+new'));
    expect(screen.getByText(en('dialog.compare.diffTruncated'))).toBeTruthy();
  });

  it('compares a trunk through its remote ref and names both sides by their branch names', async () => {
    const h = fakeHost({ compare: result });
    render(<Harness host={h.host} open="compare" select="alice/fix/typo" />);
    fireEvent.change(await screen.findByLabelText(en('dialog.compare.b')), { target: { value: 'main' } });
    fireEvent.click(screen.getByRole('button', { name: en('dialog.compare.run') }));
    await waitFor(() => expect(h.count('compare')).toBe(1));
    expect(h.calls.find((c) => c.method === 'compare')?.params).toEqual({
      repo: REPO,
      a: 'refs/heads/alice/fix/typo',
      b: 'refs/remotes/origin/main',
    });
    await screen.findByText('Only on A');
    expect(screen.getByText(en('dialog.compare.onlyB', { name: 'main' }))).toBeTruthy();
  });

  it('does not call fileDiff when the host opened the diff', async () => {
    const h = await run();
    await screen.findByText('Only on A');
    h.openDiff.mockResolvedValueOnce(true);
    fireEvent.click(screen.getByText('src/a.ts'));
    await waitFor(() => expect(h.openDiff).toHaveBeenCalledTimes(1));
    expect(h.count('fileDiff')).toBe(0);
  });
});

describe('CreateFolder', () => {
  const type = async (name: string) => {
    const input = (await screen.findByLabelText(en('dialog.create.branch'))) as HTMLInputElement;
    fireEvent.input(input, { target: { value: name } });
    return input;
  };

  it('prefills the folder from defaultFolder and keeps an edited path', async () => {
    const h = fakeHost({ createFolder: { ok: true, reason: '', files: [], path: '/x', logError: '' } });
    render(<Harness host={h.host} open="create" select="alice/fix/typo" />);
    await type('feature/new');
    const path = screen.getByLabelText(en('dialog.create.path')) as HTMLInputElement;
    await waitFor(() => expect(path.value).toBe('/demo/repo-feature-new'));
    fireEvent.input(path, { target: { value: '/work/mine' } });
    await type('feature/other');
    await waitFor(() => expect(h.calls.filter((c) => c.method === 'defaultFolder').length).toBeGreaterThanOrEqual(2));
    expect(path.value).toBe('/work/mine');
    // the trunk of the selected branch is preselected
    expect((screen.getByLabelText(en('dialog.create.trunk')) as HTMLSelectElement).value).toBe('main');
  });

  it('shows the reason of a refusal', async () => {
    const h = fakeHost({ createFolder: { ok: false, reason: 'branch-exists', files: [], path: '', logError: '' } });
    render(<Harness host={h.host} open="create" />);
    await type('feature/new');
    await waitFor(() => expect((screen.getByLabelText(en('dialog.create.path')) as HTMLInputElement).value).not.toBe(''));
    fireEvent.click(screen.getByRole('button', { name: en('dialog.create.submit') }));
    await waitFor(() => expect(screen.getByRole('dialog').textContent).toContain(en('create.reason.branch-exists')));
  });

  it('shows the folder and the detail of a refusal', async () => {
    const h = fakeHost({
      createFolder: {
        ok: false,
        reason: 'git-failed',
        files: [],
        path: '/demo/repo-feature-new',
        detail: 'branch feature/new was created at abc1234 and kept',
        logError: '',
      },
    });
    render(<Harness host={h.host} open="create" />);
    await type('feature/new');
    await waitFor(() => expect((screen.getByLabelText(en('dialog.create.path')) as HTMLInputElement).value).not.toBe(''));
    fireEvent.click(screen.getByRole('button', { name: en('dialog.create.submit') }));
    const d = screen.getByRole('dialog');
    await waitFor(() => expect(d.textContent).toContain(en('create.reason.git-failed')));
    expect(d.textContent).toContain('/demo/repo-feature-new');
    expect(d.textContent).toContain('was created at abc1234 and kept');
  });

  it('shows the new path and the log error, then reads the view again', async () => {
    const h = fakeHost({ createFolder: { ok: true, reason: '', files: [], path: '/demo/repo-feature-new', logError: 'log is read-only' } });
    render(<Harness host={h.host} open="create" />);
    await type('feature/new');
    await waitFor(() => expect((screen.getByLabelText(en('dialog.create.path')) as HTMLInputElement).value).not.toBe(''));
    const before = h.count('view');
    fireEvent.click(screen.getByRole('button', { name: en('dialog.create.submit') }));
    await waitFor(() => expect(screen.getByRole('dialog').textContent).toContain('log is read-only'));
    expect(screen.getByRole('dialog').textContent).toContain('/demo/repo-feature-new');
    await waitFor(() => expect(h.count('view')).toBeGreaterThan(before));
    expect(h.calls.find((c) => c.method === 'createFolder')?.params).toEqual({
      repo: REPO,
      trunk: 'main',
      branch: 'feature/new',
      path: '/demo/repo-feature-new',
    });
  });

  it('sends one call on a double click', async () => {
    let release: (v: unknown) => void = () => undefined;
    const h = fakeHost({ createFolder: () => new Promise((r) => (release = r)) });
    render(<Harness host={h.host} open="create" />);
    await type('feature/new');
    await waitFor(() => expect((screen.getByLabelText(en('dialog.create.path')) as HTMLInputElement).value).not.toBe(''));
    const submit = screen.getByRole('button', { name: en('dialog.create.submit') });
    fireEvent.click(submit);
    fireEvent.click(submit);
    await waitFor(() => expect(h.count('createFolder')).toBe(1));
    await act(async () => release({ ok: true, reason: '', files: [], path: '/p', logError: '' }));
    expect(h.count('createFolder')).toBe(1);
  });
});

describe('New folder for an existing branch', () => {
  it('prefills the folder, then creates it for that branch with existing: true', async () => {
    const h = fakeHost({ createFolder: { ok: true, reason: '', files: [], path: '/w/x', detail: '', logError: '' } });
    render(<Harness host={h.host} open="createExisting" select="carol/feature/search" />);
    const d = await screen.findByRole('dialog');
    expect(d.textContent).toContain('carol/feature/search');
    const path = screen.getByLabelText(en('dialog.create.path')) as HTMLInputElement;
    await waitFor(() => expect(path.value).toBe('/demo/repo-carol-feature-search'));
    expect(h.calls.find((c) => c.method === 'defaultFolder')?.params).toEqual({ repo: REPO, branch: 'carol/feature/search' });
    expect(screen.queryByLabelText(en('dialog.create.trunk'))).toBeNull();
    expect(h.count('createFolder')).toBe(0);
    fireEvent.click(screen.getByRole('button', { name: en('dialog.create.submit') }));
    await waitFor(() => expect(h.count('createFolder')).toBe(1));
    expect(h.calls.find((c) => c.method === 'createFolder')?.params).toEqual({
      repo: REPO,
      branch: 'carol/feature/search',
      path: '/demo/repo-carol-feature-search',
      existing: true,
    });
    await waitFor(() => expect(d.textContent).toContain(en('dialog.create.done')));
  });

  it('shows where the branch is checked out when the core refuses', async () => {
    const h = fakeHost({
      createFolder: { ok: false, reason: 'checked-out-elsewhere', files: [], path: '/w/other', detail: '', logError: '' },
    });
    render(<Harness host={h.host} open="createExisting" select="carol/feature/search" />);
    const path = (await screen.findByLabelText(en('dialog.create.path'))) as HTMLInputElement;
    await waitFor(() => expect(path.value).not.toBe(''));
    fireEvent.click(screen.getByRole('button', { name: en('dialog.create.submit') }));
    const d = screen.getByRole('dialog');
    await waitFor(() => expect(d.textContent).toContain(en('create.reason.checked-out-elsewhere')));
    expect(d.textContent).toContain('/w/other');
  });
});

describe('dialogs keep the repository they were opened for', () => {
  // A harness whose host can move the page to another repository while a dialog is open.
  function moving(open: DialogName, select: string, over: Record<string, unknown> = {}) {
    let refresh: () => void = () => undefined;
    let list = [REPO];
    const h = fakeHost({ repos: () => list, ...over });
    h.host.onRefresh = (cb) => (refresh = cb);
    render(<Harness host={h.host} open={open} select={select} />);
    const moveAway = async () => {
      list = ['/other/repo'];
      await act(async () => refresh());
      await waitFor(() => expect(h.calls.some((c) => c.method === 'view' && c.params.repo === '/other/repo')).toBe(true));
    };
    return { h, moveAway };
  }

  it('remove: the confirm is disabled once the page shows another repository', async () => {
    const { h, moveAway } = moving('remove', 'alice/fix/typo', { removeFolder: removeOk });
    const confirm = (await screen.findByRole('button', { name: en('dialog.remove.confirm') })) as HTMLButtonElement;
    await moveAway();
    await waitFor(() => expect(confirm.disabled).toBe(true));
    expect(screen.getByRole('dialog').textContent).toContain(en('dialog.repoChanged'));
    fireEvent.click(confirm);
    expect(h.count('removeFolder')).toBe(0);
  });

  it('new folder for a branch: the submit is disabled once the page shows another repository', async () => {
    const { h, moveAway } = moving('createExisting', 'carol/feature/search', {
      createFolder: { ok: true, reason: '', files: [], path: '/x', detail: '', logError: '' },
    });
    const path = (await screen.findByLabelText(en('dialog.create.path'))) as HTMLInputElement;
    await waitFor(() => expect(path.value).not.toBe(''));
    await moveAway();
    const submit = screen.getByRole('button', { name: en('dialog.create.submit') }) as HTMLButtonElement;
    await waitFor(() => expect(submit.disabled).toBe(true));
    expect(screen.getByRole('dialog').textContent).toContain(en('dialog.repoChanged'));
    fireEvent.click(submit);
    expect(h.count('createFolder')).toBe(0);
  });
});

describe('Assign to a trunk', () => {
  it('calls assign with the branch and trunk, then reads the view again', async () => {
    const h = fakeHost({ assign: () => mockView(REPO).settings });
    function Assign() {
      const { state, actions } = useApp(h.host);
      const done = useRef(false);
      useEffect(() => {
        if (done.current || !state.view) return;
        done.current = true;
        actions.assign('frank/spike/cache', 'develop');
      }, [state.view]);
      return <span>{state.busy ? 'busy' : 'idle'}</span>;
    }
    render(<Assign />);
    await waitFor(() => expect(h.count('assign')).toBe(1));
    expect(h.calls.find((c) => c.method === 'assign')?.params).toEqual({ repo: REPO, branch: 'frank/spike/cache', trunk: 'develop' });
    await waitFor(() => expect(h.count('view')).toBeGreaterThanOrEqual(3));
  });
});

describe('AlignReport', () => {
  it('lists updated and skipped branches with reasons, files and the log error', async () => {
    const h = fakeHost({
      align: {
        fetch: { ok: false, attempts: 2, error: 'network down', at: 0 },
        updated: [{ branch: 'bob/fix/timeout', from: 'abc1234', to: 'def5678', folder: '/demo/repo-timeout' }],
        skipped: [{ branch: 'alice/feature/login', reason: 'dirty', files: ['src/login.ts'], detail: 'skip detail' }],
        logError: 'cannot write log',
      },
    });
    render(<Harness host={h.host} open="align" />);
    const d = await screen.findByRole('dialog');
    expect(d.textContent).toContain('bob/fix/timeout');
    expect(d.textContent).toContain('abc1234');
    expect(d.textContent).toContain('def5678');
    expect(d.textContent).toContain('/demo/repo-timeout');
    expect(d.textContent).toContain(en('align.skip.dirty'));
    expect(d.textContent).toContain('src/login.ts');
    expect(d.textContent).toContain('skip detail');
    expect(d.textContent).toContain('network down');
    expect(d.textContent).toContain('cannot write log');
  });
});

describe('OpLog', () => {
  it('asks for 200 entries and lists them newest first', async () => {
    const entry = (time: number, branch: string, result: string) => ({
      time,
      repo: REPO,
      op: 'align',
      branch,
      folder: '/f',
      command: [],
      before: '',
      after: '',
      result,
      detail: 'd-' + branch,
    });
    const h = fakeHost({ ops: [entry(100, 'old/one', 'ok'), entry(300, 'new/one', 'dirty')] });
    render(<Harness host={h.host} open="oplog" />);
    await screen.findByText('new/one');
    expect(h.calls.find((c) => c.method === 'ops')?.params).toEqual({ repo: REPO, limit: 200 });
    const rows = screen.getAllByRole('row').slice(1);
    expect(within(rows[0]).getByText('new/one')).toBeTruthy();
    expect(within(rows[1]).getByText('old/one')).toBeTruthy();
    expect(screen.getByText('d-new/one')).toBeTruthy();
  });

  it('writes a links-failed result as the reason text followed by the error', async () => {
    const entry = {
      time: 5,
      repo: REPO,
      op: 'remove-folder',
      branch: 'x',
      folder: '/f',
      command: [],
      before: '',
      after: '',
      result: 'links-failed: access denied',
      detail: '',
    };
    const h = fakeHost({ ops: [entry] });
    render(<Harness host={h.host} open="oplog" />);
    await screen.findByText('x');
    expect(screen.getByRole('dialog').textContent).toContain(en('remove.reason.links-failed') + ': access denied');
  });

  it('writes a failed git step as the reason text followed by what git said', async () => {
    const entry = {
      time: 5,
      repo: REPO,
      op: 'switch',
      branch: 'x',
      folder: '/f',
      command: [],
      before: '',
      after: '',
      result: 'git-failed: error: would be overwritten',
      detail: '',
    };
    const h = fakeHost({ ops: [entry] });
    render(<Harness host={h.host} open="oplog" />);
    await screen.findByText('x');
    expect(screen.getByRole('dialog').textContent).toContain(en('switch.reason.git-failed') + ': error: would be overwritten');
  });
});

describe('dialogs in general', () => {
  const NAMES: DialogName[] = ['align', 'create', 'createExisting', 'remove', 'compare', 'settings', 'oplog'];
  for (const locale of ['zh-cn', 'en']) {
    for (const name of NAMES) {
      it(`${name} has no push, pull, sync, merge or rebase button in ${locale}`, async () => {
        const h = fakeHost(
          { align: { fetch: { ok: true, attempts: 1, error: '', at: 0 }, updated: [], skipped: [], logError: '' } },
          locale,
        );
        render(<Harness host={h.host} open={name} select="alice/fix/typo" />);
        await screen.findByRole('dialog');
        await waitFor(() => expect(screen.getAllByRole('button').length).toBeGreaterThan(0));
        for (const b of screen.getAllByRole('button')) {
          expect(b.textContent ?? '', b.outerHTML).not.toMatch(FORBIDDEN);
          expect(b.getAttribute('title') ?? '').not.toMatch(FORBIDDEN);
        }
      });
    }
  }

  it('closes with the close button', async () => {
    const h = fakeHost();
    render(<Harness host={h.host} open="oplog" />);
    fireEvent.click(await screen.findByRole('button', { name: en('app.close') }));
    expect(screen.queryByRole('dialog')).toBeNull();
  });
});

describe('repository tabs "+"', () => {
  it('discovers, then adds, without window.prompt', async () => {
    const prompt = vi.spyOn(window, 'prompt').mockReturnValue(null);
    const h = fakeHost({ discover: { repo: '/found/repo' } });
    const onAdd = vi.fn();
    render(<RepoTabs t={en} host={h.host} repos={[REPO]} repo={REPO} onSelect={() => undefined} onAdd={onAdd} />);
    fireEvent.click(screen.getByRole('button', { name: en('repo.add') }));
    fireEvent.input(screen.getByLabelText(en('repo.addPrompt')), { target: { value: ' /some/sub/folder ' } });
    fireEvent.click(screen.getByRole('button', { name: en('repo.addSubmit') }));
    await waitFor(() => expect(onAdd).toHaveBeenCalledWith('/found/repo'));
    expect(h.calls.find((c) => c.method === 'discover')?.params).toEqual({ path: '/some/sub/folder' });
    expect(prompt).not.toHaveBeenCalled();
  });

  it('shows the error text when discover fails', async () => {
    const h = fakeHost({ discover: new HostError('not-repo', 'nope') });
    const onAdd = vi.fn();
    render(<RepoTabs t={en} host={h.host} repos={[]} repo="" onSelect={() => undefined} onAdd={onAdd} />);
    fireEvent.click(screen.getByRole('button', { name: en('repo.add') }));
    fireEvent.input(screen.getByLabelText(en('repo.addPrompt')), { target: { value: '/x' } });
    fireEvent.click(screen.getByRole('button', { name: en('repo.addSubmit') }));
    await screen.findByText(new RegExp(en('code.not-repo')));
    expect(onAdd).not.toHaveBeenCalled();
  });
});

describe('results after the dialog closed (page notice)', () => {
  const closeNow = () => fireEvent.keyDown(document, { key: 'Escape' });
  const notice = () => screen.getByTestId('notice').textContent ?? '';

  it('says a remove that ran out of time is still running, not that it was not done', async () => {
    let fail: (e: unknown) => void = () => undefined;
    const h = fakeHost({ removeFolder: () => new Promise((_ok, bad) => (fail = bad)) });
    render(<Harness host={h.host} open="remove" select="alice/fix/typo" />);
    fireEvent.click(await screen.findByRole('button', { name: en('dialog.remove.confirm') }));
    await waitFor(() => expect(h.count('removeFolder')).toBe(1));
    closeNow();
    await act(async () => fail(new HostError('still-running', 'removeFolder is still running after 30 min')));
    await waitFor(() => expect(screen.getByTestId('notice')).toBeTruthy());
    expect(notice()).toMatch(/still running/i);
    expect(notice()).not.toMatch(/not done/);
  });

  it('shows a failed remove that ended after the dialog was closed', async () => {
    let fail: (e: unknown) => void = () => undefined;
    const h = fakeHost({ removeFolder: () => new Promise((_ok, bad) => (fail = bad)) });
    render(<Harness host={h.host} open="remove" select="alice/fix/typo" />);
    fireEvent.click(await screen.findByRole('button', { name: en('dialog.remove.confirm') }));
    await waitFor(() => expect(h.count('removeFolder')).toBe(1));
    closeNow();
    await act(async () => fail(new HostError('git-failed', 'boom')));
    await waitFor(() => expect(screen.getByTestId('notice')).toBeTruthy());
    expect(notice()).toContain(en('notice.op.remove'));
    expect(notice()).toContain('alice/fix/typo');
    expect(notice()).toContain(en('code.git-failed'));
  });

  it('shows the reason of a refusal that arrived after the dialog was closed', async () => {
    let done: (v: unknown) => void = () => undefined;
    const h = fakeHost({ removeFolder: () => new Promise((r) => (done = r)) });
    render(<Harness host={h.host} open="remove" select="alice/fix/typo" />);
    fireEvent.click(await screen.findByRole('button', { name: en('dialog.remove.confirm') }));
    await waitFor(() => expect(h.count('removeFolder')).toBe(1));
    closeNow();
    await act(async () => done({ ...removeOk, ok: false, reason: 'locked' }));
    await waitFor(() => expect(screen.getByTestId('notice')).toBeTruthy());
    expect(notice()).toContain(en('remove.reason.locked'));
  });

  it('shows a success with its log error after the dialog was closed', async () => {
    let done: (v: unknown) => void = () => undefined;
    const h = fakeHost({ removeFolder: () => new Promise((r) => (done = r)) });
    render(<Harness host={h.host} open="remove" select="alice/fix/typo" />);
    fireEvent.click(await screen.findByRole('button', { name: en('dialog.remove.confirm') }));
    await waitFor(() => expect(h.count('removeFolder')).toBe(1));
    closeNow();
    await act(async () => done(removeOk));
    await waitFor(() => expect(screen.getByTestId('notice')).toBeTruthy());
    expect(notice()).toContain('disk is full');
  });

  it('shows a failed clearBackups after Settings was closed', async () => {
    let fail: (e: unknown) => void = () => undefined;
    const h = fakeHost({ clearBackups: () => new Promise((_ok, bad) => (fail = bad)) });
    render(<Harness host={h.host} open="settings" />);
    fireEvent.click(await screen.findByRole('button', { name: en('dialog.settings.clearBackups') }));
    fireEvent.click(screen.getByRole('button', { name: en('dialog.settings.clearYes') }));
    await waitFor(() => expect(h.count('clearBackups')).toBe(1));
    closeNow();
    await act(async () => fail(new HostError('internal', 'x')));
    await waitFor(() => expect(screen.getByTestId('notice')).toBeTruthy());
    expect(notice()).toContain(en('notice.op.clearBackups'));
    expect(notice()).toContain(en('code.internal'));
  });

  it('shows a failed setRole after Settings was closed', async () => {
    let fail: (e: unknown) => void = () => undefined;
    const h = fakeHost({ setRole: () => new Promise((_ok, bad) => (fail = bad)) });
    render(<Harness host={h.host} open="settings" />);
    fireEvent.input(await screen.findByLabelText(en('dialog.settings.branch')), { target: { value: 'release/2.0' } });
    fireEvent.click(screen.getByRole('button', { name: en('dialog.settings.apply') }));
    await waitFor(() => expect(h.count('setRole')).toBe(1));
    closeNow();
    await act(async () => fail(new HostError('bad-params', 'nope')));
    await waitFor(() => expect(screen.getByTestId('notice')).toBeTruthy());
    expect(notice()).toContain(en('notice.op.setRole'));
    expect(notice()).toContain('release/2.0');
    expect(notice()).toContain(en('code.bad-params'));
  });

  it('shows the result in the dialog, and no page notice, while the dialog is open', async () => {
    const h = fakeHost({ removeFolder: removeOk, createFolder: new HostError('internal', 'x') });
    render(<Harness host={h.host} open="remove" select="alice/fix/typo" />);
    fireEvent.click(await screen.findByRole('button', { name: en('dialog.remove.confirm') }));
    await waitFor(() => expect(screen.getByRole('dialog').textContent).toContain('/cfg/backups/typo-1'));
    expect(screen.queryByTestId('notice')).toBeNull();
  });

  it('shows a failed setRole in Settings, and no page notice, while it is open', async () => {
    const h = fakeHost({ setRole: new HostError('bad-params', 'nope') });
    render(<Harness host={h.host} open="settings" />);
    fireEvent.input(await screen.findByLabelText(en('dialog.settings.branch')), { target: { value: 'x/y' } });
    fireEvent.click(screen.getByRole('button', { name: en('dialog.settings.apply') }));
    await waitFor(() => expect(screen.getByRole('dialog').textContent).toContain(en('code.bad-params')));
    expect(screen.queryByTestId('notice')).toBeNull();
  });
});
