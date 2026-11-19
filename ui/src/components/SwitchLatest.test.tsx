import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/preact';
import { App } from './App';
import { makeT } from '../i18n';
import type { Key } from '../i18n';
import { reasonText } from '../reasons';
import { HostError } from '../host/types';
import type { Host } from '../host/types';
import { mockView } from '../host/mock';
import type { Shelf, SwitchResult, SwitchState, View } from '../types';

const en = makeT('en');
const zh = makeT('zh-cn');
const REPO = '/demo/repo';

// named finds a branch name, which BranchName writes in two parts.
const named = (n: string) => (_: string, el: Element | null) =>
  !!el?.classList.contains('bname') &&
  (el.querySelector('.bname-pre')?.textContent ?? '') + el.querySelector('.bname-last')!.textContent === n;

const sw = (over: Partial<SwitchResult> = {}): SwitchResult => ({
  ok: true,
  reason: '',
  files: [],
  path: REPO,
  detail: '',
  logError: '',
  shelved: null,
  restored: null,
  keptShelf: null,
  fastForwarded: false,
  aheadNote: '',
  ...over,
});
const shelf = (over: Partial<Shelf> = {}): Shelf => ({
  folder: REPO,
  branch: 'main',
  hash: 'a'.repeat(40),
  base: 'b'.repeat(40),
  time: 1_700_000_000,
  files: ['src/app.ts', 'notes.txt'],
  why: '',
  ...over,
});

interface Opts {
  answer?: (p: Record<string, unknown>) => SwitchResult;
  state?: SwitchState;
  view?: (v: View) => View;
  mode?: 'wide' | 'sidebar';
}

function switchHost(o: Opts = {}) {
  const calls: Array<{ method: string; params: Record<string, unknown> }> = [];
  const host: Host = {
    async call<T>(method: string, params?: object): Promise<T> {
      const p = (params ?? {}) as Record<string, unknown>;
      calls.push({ method, params: p });
      switch (method) {
        case 'repos':
          return [REPO] as T;
        case 'view':
          return (o.view ? o.view(mockView(REPO)) : mockView(REPO)) as T;
        case 'fetch':
          return { ok: true, attempts: 1, error: '', at: 1 } as T;
        case 'getGlobal':
          return { repos: [REPO], editor: 'code', theme: 'auto' } as T;
        case 'version':
          return { core: 'x', git: '2.45', gitOK: true } as T;
        case 'switchState':
          return (o.state ?? { previous: '', shelves: [] }) as T;
        case 'switch':
          return (o.answer ? o.answer(p) : sw()) as T;
        default:
          throw new HostError('unknown-method', method);
      }
    },
    openFolder: async () => undefined,
    copy: async () => undefined,
    openDiff: async () => false,
    openWide: () => undefined,
    mode: o.mode ?? 'wide',
    locale: 'en',
    theme: () => 'dark',
    onTheme: () => undefined,
  };
  const after = (i: number) => calls.slice(i).map((c) => c.method).filter((m) => m !== 'view' && m !== 'switchState');
  return { host, calls, after, writes: () => calls.filter((c) => c.method === 'switch') };
}

async function openTrunk(host: Host, trunk = 'main') {
  render(<App host={host} />);
  await screen.findByText(named('feature/login'));
  await waitFor(() => expect(screen.queryByText('Reading the remote…')).toBeNull());
  fireEvent.click(screen.getByRole('button', { name: new RegExp(`^Trunk ${trunk} ?\\(\\d+\\)$`) }));
}

afterEach(cleanup);

describe('switch the main folder to the latest', () => {
  it('offers the trunk and its test branch at their latest, and sends nothing before a click', async () => {
    const h = switchHost();
    await openTrunk(h.host);
    expect(screen.getByRole('button', { name: 'Switch to main latest' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Switch to test latest' })).toBeTruthy();
    expect(h.writes()).toHaveLength(0);
    cleanup();
    await openTrunk(switchHost().host, 'develop'); // no test branch set
    expect(screen.getByRole('button', { name: 'Switch to develop latest' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: /Switch to test/ })).toBeNull();
  });

  it('reads the remote, then switches with one click; the result line says what happened', async () => {
    const h = switchHost({ answer: () => sw({ fastForwarded: true }) });
    await openTrunk(h.host);
    const before = h.calls.length;
    fireEvent.click(screen.getByRole('button', { name: 'Switch to test latest' }));
    expect(await screen.findByText('The main folder is now on test')).toBeTruthy();
    expect(screen.getByText('It was first brought up to date with its remote')).toBeTruthy();
    expect(h.after(before)).toEqual(['fetch', 'switch']);
    expect(h.writes()[0].params).toEqual({ repo: REPO, folder: REPO, branch: 'test', latest: true, shelve: false, remote: 'origin' });
    await waitFor(() => expect(h.calls.slice(before).filter((c) => c.method === 'switchState')).toHaveLength(1));
  });

  it('lists the files before anything is shelved, and shelves only after the confirm', async () => {
    const h = switchHost({
      answer: (p) => (p.shelve ? sw({ shelved: shelf() }) : sw({ ok: false, reason: 'dirty', files: ['src/app.ts', 'notes.txt'] })),
    });
    await openTrunk(h.host);
    const before = h.calls.length;
    fireEvent.click(screen.getByRole('button', { name: 'Switch to test latest' }));
    const dialog = await screen.findByRole('dialog', { name: 'Shelve uncommitted changes?' });
    expect(within(dialog).getByText('src/app.ts')).toBeTruthy();
    for (const b of within(dialog).getAllByRole('button')) expect(b.textContent).not.toMatch(/push|pull|sync|merge|rebase/i);
    expect(within(dialog).getByText('notes.txt')).toBeTruthy();
    expect(h.writes().map((w) => w.params.shelve)).toEqual([false]);

    fireEvent.click(within(dialog).getByRole('button', { name: 'Close' }));
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(h.writes()).toHaveLength(1);

    fireEvent.click(screen.getByRole('button', { name: 'Switch to test latest' }));
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Shelve and switch' }));
    expect(await screen.findByText('Shelved 2 uncommitted files of main; they come back when you switch back to main')).toBeTruthy();
    expect(h.writes().map((w) => w.params.shelve)).toEqual([false, false, true]);
    expect(h.writes()[2].params).toEqual({ repo: REPO, folder: REPO, branch: 'test', latest: true, shelve: true, remote: 'origin' });
    expect(h.after(before)).toEqual(['fetch', 'switch', 'fetch', 'switch', 'switch']);
  });

  it('asks first, without sending anything, when the page already knows the main folder has changes', async () => {
    const h = switchHost({
      answer: () => sw({ shelved: shelf({ branch: 'carol/feature/search', files: ['src/a.ts', 'src/b.ts', 'new.txt'] }) }),
      view: (v) => {
        v.groups[0].trunk.folder = '';
        Object.assign(v.branches['carol/feature/search'], {
          mainFolder: true,
          folder: REPO,
          dirty: { changed: 2, untracked: 1, files: ['src/a.ts', 'src/b.ts', 'new.txt'] },
        });
        return v;
      },
    });
    await openTrunk(h.host);
    const before = h.calls.length;
    fireEvent.click(screen.getByRole('button', { name: 'Switch to main latest' }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText('new.txt')).toBeTruthy();
    expect(h.after(before)).toEqual([]);
    fireEvent.click(within(dialog).getByRole('button', { name: 'Shelve and switch' }));
    await screen.findByText(/Shelved 3 uncommitted files of carol\/feature\/search/);
    expect(h.after(before)).toEqual(['fetch', 'switch']);
    expect(h.writes()[0].params).toMatchObject({ branch: 'main', latest: true, shelve: true });
  });

  it('does not offer to shelve when the main folder is already on that branch with changes', async () => {
    const h = switchHost({ answer: () => sw({ ok: false, reason: 'dirty', files: ['src/app.ts'] }) });
    await openTrunk(h.host);
    fireEvent.click(screen.getByRole('button', { name: 'Switch to main latest' }));
    expect(await screen.findByText('The main folder is already on main and has uncommitted changes; commit or stash them first, then update it')).toBeTruthy();
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(h.writes()).toHaveLength(1);
  });

  it('says where kept work is, and shows the refusals with their files', async () => {
    const why = 'main moved from 1a2b3c4d5e6f to 9f8e7d6c5b4a since the work was shelved; putting it back could conflict';
    const h = switchHost({ answer: () => sw({ keptShelf: shelf({ why }) }) });
    await openTrunk(h.host);
    fireEvent.click(screen.getByRole('button', { name: 'Switch to test latest' }));
    expect(await screen.findByText('The shelved changes of main were not put back; they are in git stash list as aaaaaaa')).toBeTruthy();
    expect(screen.getByText(why)).toBeTruthy();
    expect(screen.getByText(`git stash apply --index ${'a'.repeat(40)}`)).toBeTruthy(); // how to put them back by hand
    cleanup();

    const u = switchHost({ answer: () => sw({ ok: false, reason: 'unsaved-editors', files: ['src/login.ts'] }) });
    await openTrunk(u.host);
    fireEvent.click(screen.getByRole('button', { name: 'Switch to test latest' }));
    expect(await screen.findByText(en('switch.reason.unsaved-editors'))).toBeTruthy();
    expect(screen.getByText('src/login.ts')).toBeTruthy();
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('notes a branch that was not brought up to date because it has commits of its own', async () => {
    const h = switchHost({ answer: () => sw({ aheadNote: 'local has 2 commits not on origin/main; not fast-forwarded' }) });
    await openTrunk(h.host);
    fireEvent.click(screen.getByRole('button', { name: 'Switch to main latest' }));
    expect(await screen.findByText(en('switch.ahead'))).toBeTruthy();
    expect(screen.getByText(/local has 2 commits not on origin\/main/)).toBeTruthy();
  });
});

describe('switch back', () => {
  const away: SwitchState = {
    previous: 'alice/feature/login',
    shelves: [shelf({ branch: 'alice/feature/login', files: ['a.ts', 'b.ts', 'c.ts'], hash: 'c'.repeat(40) })],
  };

  it('shows while the main folder is away, and sends the previous branch without reading the remote', async () => {
    const h = switchHost({ state: away, answer: () => sw({ restored: away.shelves[0] }) });
    render(<App host={h.host} />);
    const back = await screen.findByRole('button', { name: 'Switch back to alice/feature/login' });
    expect(screen.getByText('3 shelved files of alice/feature/login are in git stash list as ccccccc')).toBeTruthy();
    await waitFor(() => expect(screen.queryByText('Reading the remote…')).toBeNull());
    const before = h.calls.length;
    fireEvent.click(back);
    expect(await screen.findByText('Put back the 3 shelved files of alice/feature/login')).toBeTruthy();
    expect(h.after(before)).toEqual(['switch']);
    expect(h.writes()[0].params).toEqual({ repo: REPO, folder: REPO, branch: 'alice/feature/login', latest: false, shelve: false, remote: '' });
  });

  it('is there in the sidebar too, right under its header', async () => {
    const h = switchHost({ state: away, mode: 'sidebar' });
    const { container } = render(<App host={h.host} />);
    const back = await screen.findByRole('button', { name: 'Switch back to alice/feature/login' });
    expect(back.closest('.main-folder')!.parentElement).toBe(container.querySelector('.sidebar'));
  });

  it('says why kept work will not come back on its own', async () => {
    const stuck = { previous: '', shelves: [shelf({ why: 'apply failed' })] };
    render(<App host={switchHost({ state: stuck }).host} />);
    expect(await screen.findByText('2 shelved files of main are in git stash list as aaaaaaa')).toBeTruthy();
    expect(screen.getByText('They will not be put back automatically: apply failed')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /Switch back/ })).toBeNull();
  });
});

describe('switch texts', () => {
  it('has zh and en text for every switch refusal and the new op-log entries', () => {
    const reasons = [
      'dirty', 'folder-missing', 'checked-out-elsewhere', 'no-branch', 'detached', 'hidden-changes', 'git-failed',
      'verify-failed', 'in-progress', 'shelf-failed', 'restore-failed', 'unsaved-editors',
    ];
    for (const r of reasons) {
      expect(reasonText('switch.reason', r, en), r).not.toBe(r);
      expect(reasonText('switch.reason', r, zh), r).not.toBe(r);
    }
    for (const op of ['shelve', 'unshelve', 'drop-shelf']) {
      const key = `oplog.op.${op}` as Key;
      expect(en(key)).not.toBe(key);
      expect(zh(key)).not.toBe(en(key));
    }
  });
});
