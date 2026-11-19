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
  noFastForward: '',
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
  state: 'pending',
  whyCode: '',
  why: '',
  ...over,
});
// details finds the English detail of the core, which sits folded in a <details>
const detail = (text: string) => screen.getByText(text).closest('details');

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

  it('lists what the core finds now, never the older list of the view', async () => {
    const h = switchHost({
      answer: (p) =>
        p.shelve
          ? sw({ shelved: shelf({ branch: 'carol/feature/search', files: ['src/a.ts', 'new.txt'] }) })
          : sw({ ok: false, reason: 'dirty', files: ['src/a.ts', 'new.txt'] }),
      view: (v) => {
        v.groups[0].trunk.folder = '';
        Object.assign(v.branches['carol/feature/search'], {
          mainFolder: true,
          folder: REPO,
          dirty: { changed: 1, untracked: 0, files: ['old.txt'] }, // read before new.txt was made
        });
        return v;
      },
    });
    await openTrunk(h.host);
    const before = h.calls.length;
    fireEvent.click(screen.getByRole('button', { name: 'Switch to main latest' }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText('new.txt')).toBeTruthy();
    expect(within(dialog).queryByText('old.txt')).toBeNull();
    expect(within(dialog).getByText(en('dialog.shelve.later'))).toBeTruthy();
    expect(h.after(before)).toEqual(['fetch', 'switch']);
    expect(h.writes()[0].params).toMatchObject({ branch: 'main', latest: true, shelve: false });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Shelve and switch' }));
    await screen.findByText(/Shelved 2 uncommitted files of carol\/feature\/search/);
    expect(h.after(before)).toEqual(['fetch', 'switch', 'switch']);
    expect(h.writes()[1].params).toMatchObject({ branch: 'main', latest: true, shelve: true });
  });

  it('does not offer to shelve when the main folder is already on that branch with changes', async () => {
    const h = switchHost({ answer: () => sw({ ok: false, reason: 'dirty', files: ['src/app.ts'] }) });
    await openTrunk(h.host);
    fireEvent.click(screen.getByRole('button', { name: 'Switch to main latest' }));
    expect(await screen.findByText('The main folder is already on main and has uncommitted changes; commit or stash them first, then update it')).toBeTruthy();
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(h.writes()).toHaveLength(1);
  });

  // switchOnce clicks "Switch to test latest" with the given answer of the core
  const switchOnce = async (answer: SwitchResult) => {
    const h = switchHost({ answer: () => answer });
    await openTrunk(h.host);
    fireEvent.click(screen.getByRole('button', { name: 'Switch to test latest' }));
    return h;
  };
  const APPLY = `git stash apply --index ${'a'.repeat(40)}`;

  it('says a shelf that was not put back yet comes back later, with the English detail folded', async () => {
    const why = 'main moved from 1a2b3c4d5e6f to 9f8e7d6c5b4a since the work was shelved; putting it back could conflict';
    await switchOnce(sw({ keptShelf: shelf({ state: 'pending', whyCode: 'base-moved', why }) }));
    expect(
      await screen.findByText('The shelved changes of main were not put back yet: main moved since the work was shelved, so putting it back could conflict. They are tried again on the next switch back.'),
    ).toBeTruthy();
    expect(detail(why)).toBeTruthy();
    expect(screen.queryByText(APPLY)).toBeNull(); // it still comes back on its own
  });

  it('says where kept work is and how to put it back by hand', async () => {
    await switchOnce(sw({ keptShelf: shelf({ state: 'kept', whyCode: 'apply-failed', why: 'stash apply failed' }) }));
    expect(
      await screen.findByText('The shelved changes of main were not put back: putting them back failed and nothing was changed. They are in git stash list as aaaaaaa.'),
    ).toBeTruthy();
    expect(screen.getByText(APPLY)).toBeTruthy();
    expect(detail('stash apply failed')).toBeTruthy();
  });

  // Each code says only what is true of it, and the next step that fits.
  const SHOW = `git stash show -p ${'a'.repeat(40)}`;
  const TAKEN = 'files the shelf would create are already on disk (build/out.txt, notes.txt); putting the work back would stop on them or overwrite them';
  const truths: Array<{ code: string; state: string; why?: string; says: string; step: 'apply' | 'check' | 'gone' | 'clear' | 'none' }> = [
    { code: 'path-exists', state: 'kept', why: TAKEN, says: 'The shelved changes of main are whole in git stash list as aaaaaaa, but files now sit where they would go back: build/out.txt, notes.txt.', step: 'clear' },
    { code: 'path-exists', state: 'kept', says: 'The shelved changes of main are whole in git stash list as aaaaaaa, but files now sit where they would go back.', step: 'clear' },
    { code: 'apply-partial', state: 'kept', says: 'Part of the shelved changes of main is already back in the folder, and all of them are still in git stash list as aaaaaaa.', step: 'check' },
    { code: 'apply-mismatch', state: 'kept', says: 'The shelved changes of main were applied, but the folder does not match them: part or all of the work may already be in the folder. All of it is still in git stash list as aaaaaaa.', step: 'check' },
    { code: 'entry-gone', state: 'kept', says: 'The shelved changes of main are no longer in git stash list.', step: 'gone' },
    { code: 'folder-dirty', state: 'kept', says: 'The shelved changes of main were not put back: the folder has uncommitted changes. They are in git stash list as aaaaaaa.', step: 'apply' },
    { code: 'superseded', state: 'superseded', says: 'The shelved changes of main were not put back: a newer shelf of main replaces it. They are in git stash list as aaaaaaa.', step: 'apply' },
    { code: 'check-failed', state: 'kept', says: 'The shelved changes of main were not put back automatically (the folder or git stash list could not be read); their stash commit is aaaaaaa.', step: 'none' },
    { code: 'later-code', state: 'kept', says: 'The shelved changes of main were not put back automatically (code later-code); their stash commit is aaaaaaa.', step: 'none' },
  ];
  for (const c of truths) {
    it(`says only what is true for ${c.code}${c.why ? ' (files named)' : ''}, and the next step that fits`, async () => {
      await switchOnce(sw({ keptShelf: shelf({ state: c.state, whyCode: c.code, why: c.why ?? 'English detail' }) }));
      expect(await screen.findByText(c.says)).toBeTruthy();
      expect(!!screen.queryByText(APPLY), 'apply command').toBe(c.step === 'apply' || c.step === 'gone' || c.step === 'clear');
      expect(!!screen.queryByText(en('switch.keptHow')), 'apply right away').toBe(c.step === 'apply');
      // the files in the way are moved first: the command never comes without that step before it
      expect(!!screen.queryByText(en('switch.clearFirst')), 'clear first').toBe(c.step === 'clear');
      if (c.step === 'clear') expect(screen.getByText(APPLY).parentElement!.textContent!.startsWith(en('switch.clearFirst'))).toBe(true);
      expect(!!screen.queryByText(SHOW), 'show command').toBe(c.step === 'check');
      expect(!!screen.queryByText(en('switch.checkFirst')), 'check first').toBe(c.step === 'check');
      expect(!!screen.queryByText(en('switch.goneHow')), 'only while git has it').toBe(c.step === 'gone');
      if (c.code === 'entry-gone') expect(screen.queryByText(/They are in git stash list/)).toBeNull();
    });
  }

  it('does not promise shelved work comes back when the switch failed and it was put back in place', async () => {
    await switchOnce(sw({ ok: false, reason: 'git-failed', shelved: shelf(), restored: shelf({ state: 'restored' }) }));
    expect(await screen.findByText('The 2 uncommitted files of main were shelved, then put back in place when the switch failed')).toBeTruthy();
    expect(screen.queryByText(/they come back when you switch back/)).toBeNull();
    expect(screen.queryByText('Put back the 2 shelved files of main')).toBeNull();
  });

  it('says where shelved work is kept when the switch failed and it could not be put back', async () => {
    await switchOnce(
      sw({
        ok: false,
        reason: 'restore-failed',
        shelved: shelf(),
        keptShelf: shelf({ state: 'kept', whyCode: 'folder-dirty', why: 'folder not clean' }),
        detail: `the switch failed and the work could not be put back; it is kept in git stash list as ${'a'.repeat(40)}`,
      }),
    );
    expect(await screen.findByText(en('switch.reason.restore-failed'))).toBeTruthy();
    expect(screen.getByText('The shelved changes of main were not put back: the folder has uncommitted changes. They are in git stash list as aaaaaaa.')).toBeTruthy();
    expect(screen.getByText(APPLY)).toBeTruthy();
    expect(screen.queryByText(/they come back when you switch back/)).toBeNull();
  });

  it('never offers to put back work that is already back', async () => {
    await switchOnce(sw({ restored: shelf({ state: 'restored-entry-kept', whyCode: 'drop-shifted', why: 'git dropped stash@{0} (1234)' }) }));
    expect(await screen.findByText('Put back the 2 shelved files of main')).toBeTruthy();
    expect(screen.getByText('Its stash entry was kept: git dropped another stash entry instead, which was stored again')).toBeTruthy();
    expect(screen.queryByText(/git stash apply/)).toBeNull();
  });

  it('falls back to the code for a state or reason this page does not know', async () => {
    await switchOnce(sw({ keptShelf: shelf({ state: 'later-state', whyCode: 'later-code', why: 'something new' }) }));
    expect(await screen.findByText(/code later-code/)).toBeTruthy();
    expect(detail('something new')).toBeTruthy();
  });

  it('says why a branch was not brought up to date, for every code', async () => {
    const cases: Array<[string, string]> = [
      ['ahead', en('switch.noff.ahead')],
      ['no-upstream', en('switch.noff.no-upstream')],
      ['shelf-pending', en('switch.noff.shelf-pending')],
      ['later', 'It was not brought up to date (later)'],
    ];
    for (const [code, text] of cases) {
      await switchOnce(sw({ noFastForward: code, aheadNote: code === 'ahead' ? 'local has 2 commits not on origin/main; not fast-forwarded' : '' }));
      expect(await screen.findByText(text), code).toBeTruthy();
      cleanup();
    }
  });

  it('says the folder is at what was read before when the remote could not be read first', async () => {
    const h = switchHost({ answer: () => sw() });
    const real = h.host.call.bind(h.host);
    h.host.call = async <T,>(method: string, params?: object): Promise<T> =>
      method === 'fetch' ? ({ ok: false, attempts: 2, error: 'offline', at: 0 } as T) : real<T>(method, params);
    await openTrunk(h.host);
    fireEvent.click(screen.getByRole('button', { name: 'Switch to test latest' }));
    expect(await screen.findByText('The remote could not be read first, so test is at the version read before, which may not be the newest')).toBeTruthy();
  });

  it('shows the refusals with their files', async () => {

    const u = switchHost({ answer: () => sw({ ok: false, reason: 'unsaved-editors', files: ['src/login.ts'] }) });
    await openTrunk(u.host);
    fireEvent.click(screen.getByRole('button', { name: 'Switch to test latest' }));
    expect(await screen.findByText(en('switch.reason.unsaved-editors'))).toBeTruthy();
    expect(screen.getByText('src/login.ts')).toBeTruthy();
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('notes a branch that was not brought up to date because it has commits of its own', async () => {
    const note = 'local has 2 commits not on origin/main; not fast-forwarded';
    const h = switchHost({ answer: () => sw({ noFastForward: 'ahead', aheadNote: note }) });
    await openTrunk(h.host);
    fireEvent.click(screen.getByRole('button', { name: 'Switch to main latest' }));
    expect(await screen.findByText(en('switch.noff.ahead'))).toBeTruthy();
    expect(detail(note)).toBeTruthy();
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
    expect(screen.getByText('They come back when the main folder is switched back to alice/feature/login')).toBeTruthy();
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

  it('points a partly applied shelf to git status first, without the apply command', async () => {
    const partial = { previous: '', shelves: [shelf({ state: 'kept', whyCode: 'apply-partial', why: 'half way' })] };
    render(<App host={switchHost({ state: partial }).host} />);
    expect(await screen.findByText(en('switch.checkFirst'))).toBeTruthy();
    expect(screen.getByText(`git stash show -p ${'a'.repeat(40)}`)).toBeTruthy();
    expect(screen.queryByText(`git stash apply --index ${'a'.repeat(40)}`)).toBeNull();
  });

  it('names the files in the way of a shelf, and has them moved before the apply command', async () => {
    const taken = {
      previous: '',
      shelves: [shelf({ state: 'kept', whyCode: 'path-exists', why: 'files the shelf would create are already on disk (build/out.txt); putting the work back would stop on them or overwrite them' })],
    };
    render(<App host={switchHost({ state: taken }).host} />);
    expect(await screen.findByText('They will not be put back automatically: files now sit where it would put files back: build/out.txt')).toBeTruthy();
    const command = screen.getByText(`git stash apply --index ${'a'.repeat(40)}`);
    expect(command.parentElement!.textContent!.startsWith(en('switch.clearFirst'))).toBe(true);
    expect(screen.queryByText(en('switch.keptHow'))).toBeNull();
  });

  it('says why kept work will not come back on its own, and how to put it back by hand', async () => {
    const stuck = { previous: '', shelves: [shelf({ state: 'kept', whyCode: 'apply-failed', why: 'apply failed' })] };
    render(<App host={switchHost({ state: stuck }).host} />);
    expect(await screen.findByText('2 shelved files of main are in git stash list as aaaaaaa')).toBeTruthy();
    expect(screen.getByText('They will not be put back automatically: putting them back failed and nothing was changed')).toBeTruthy();
    expect(screen.getByText(`git stash apply --index ${'a'.repeat(40)}`)).toBeTruthy();
    expect(screen.queryByRole('button', { name: /Switch back/ })).toBeNull();
  });

  it('lists shelved work found in git stash list without a record, and work already back without a command', async () => {
    const found: SwitchState = {
      previous: '',
      shelves: [
        shelf({ folder: '', branch: 'feature/login', hash: 'd'.repeat(40), files: [], state: 'orphan', whyCode: 'no-record', why: 'no record' }),
        shelf({ folder: '/elsewhere', branch: 'v2', hash: 'e'.repeat(40), state: 'kept' }), // another folder: not this line's
        shelf({ hash: 'f'.repeat(40), state: 'restored-entry-kept', whyCode: 'entry-ambiguous', why: 'listed twice' }),
      ],
    };
    render(<App host={switchHost({ state: found }).host} />);
    expect(await screen.findByText('Shelved work of feature/login found in git stash list as ddddddd')).toBeTruthy();
    expect(screen.getByText('Found in git stash list; not put back automatically')).toBeTruthy();
    expect(screen.getByText(`git stash apply --index ${'d'.repeat(40)}`)).toBeTruthy();
    expect(screen.queryByText(/eeeeeee/)).toBeNull();
    expect(screen.getByText('They are already back in the folder; only the stash entry is left: it is listed more than once in git stash list, so no entry was dropped')).toBeTruthy();
    expect(screen.queryByText(`git stash apply --index ${'f'.repeat(40)}`)).toBeNull();
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
    for (const op of ['shelve', 'unshelve', 'drop-shelf', 'store-entry']) {
      const key = `oplog.op.${op}` as Key;
      expect(en(key)).not.toBe(key);
      expect(zh(key)).not.toBe(en(key));
    }
    const prefixed = (prefix: string, codes: string[]) => codes.map((c) => `${prefix}.${c}` as Key);
    const keys = [
      ...prefixed('shelf.state', ['pending', 'kept', 'superseded', 'restored', 'restored-entry-kept', 'orphan']),
      ...prefixed('shelf.why', [
        'base-moved', 'not-on-branch', 'folder-dirty', 'path-exists', 'check-failed', 'entry-gone', 'apply-failed',
        'apply-partial', 'apply-mismatch', 'entry-ambiguous', 'drop-failed', 'drop-shifted', 'superseded', 'no-record',
      ]),
      ...prefixed('switch.noff', ['ahead', 'no-upstream', 'shelf-pending']),
    ];
    for (const key of keys) {
      expect(en(key), key).not.toBe(key);
      expect(zh(key), key).not.toBe(en(key));
    }
  });
});
