import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/preact';
import { useEffect, useMemo, useRef } from 'preact/hooks';
import { Dialogs } from './Dialogs';
import { RepoTabs } from '../RepoTabs';
import { makeT } from '../../i18n';
import { mockView } from '../../host/mock';
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
  return <Dialogs host={host} t={t} state={state} actions={actions} />;
}

const en = makeT('en');
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
    expect(h.calls.find((c) => c.method === 'removeFolder')?.params).toEqual({ repo: REPO, branch: 'alice/fix/typo' });
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
    expect(h.calls.find((c) => c.method === 'compare')?.params).toEqual({ repo: REPO, a: 'alice/fix/typo', b: 'bob/fix/timeout' });
    fireEvent.click(screen.getByText('src/a.ts'));
    await waitFor(() => expect(h.openDiff).toHaveBeenCalledWith(REPO, 'alice/fix/typo', 'bob/fix/timeout', 'src/a.ts'));
    await waitFor(() => expect(h.count('fileDiff')).toBe(1));
    await waitFor(() => expect(screen.getByRole('dialog').textContent).toContain('+new'));
    expect(screen.getByText(en('dialog.compare.diffTruncated'))).toBeTruthy();
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
});

describe('dialogs in general', () => {
  const NAMES: DialogName[] = ['align', 'create', 'remove', 'compare', 'settings', 'oplog'];
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
