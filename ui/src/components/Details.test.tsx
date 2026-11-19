import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/preact';
import { Details } from './Details';
import { layoutView } from './graphFixture';
import { makeT } from '../i18n';
import type { Actions, AppState } from '../state';
import { dateTime } from '../format';
import type { TrunkView, View } from '../types';
import type { Host } from '../host/types';

const t = makeT('en');
const host = {} as Host; // never called: no news is open in these tests
const actions = () => new Proxy({} as Actions, { get: () => vi.fn() });
const state = (over: Partial<AppState>) => ({ selected: '', selectedTrunk: '', pick: null, busy: false, switchResult: null, ...over }) as AppState;

afterEach(cleanup);

describe('Details', () => {
  it('shows a trunk commit with the branches that start there', () => {
    const view = layoutView();
    view.branches.A.forkHash = 'F1';
    view.branches.B.forkHash = 'F1';
    render(
      <Details host={host} t={t} view={view} actions={actions()} state={state({ pick: { kind: 'trunk', trunk: 'main', hash: 'F1', time: 1_700_000_090, subject: 'Fix it' } })} />,
    );
    expect(screen.getByText('F1')).toBeTruthy();
    expect(screen.getByText('Fix it')).toBeTruthy();
    expect(screen.getByText('A')).toBeTruthy();
    expect(screen.getByText('B')).toBeTruthy();
    expect(screen.queryByText('C')).toBeNull();
  });

  it('says so when no branch starts at a trunk commit', () => {
    render(<Details host={host} t={t} view={layoutView()} actions={actions()} state={state({ pick: { kind: 'trunk', trunk: 'main', hash: 'H', time: 0, subject: '' } })} />);
    expect(screen.getByText('No branch starts here')).toBeTruthy();
  });

  it('shows the picked commit of a branch above the branch details', () => {
    const view = layoutView();
    view.branches.A.test = { state: 'some', matched: 1, total: 2, per: [{ hash: 'a2', subject: 'commit a2', time: 1_700_000_099, author: 'alice', by: 'patch' }, { hash: 'a1', subject: 'commit a1', time: 1_700_000_098, author: 'alice', by: '' }] };
    const { container } = render(<Details host={host} t={t} view={view} actions={actions()} state={state({ selected: 'A', pick: { kind: 'commit', branch: 'A', hash: 'a2' } })} />);
    expect(container.querySelector('.detail-commit code')!.textContent).toBe('a2');
    expect(container.querySelector('.detail-commit')!.textContent).toContain('same change');
    expect(screen.getAllByText('commit a2').length).toBeGreaterThan(0);
    expect(container.querySelector('.detail-name strong')!.textContent).toBe('A');
  });
});

describe('trunk details', () => {
  const trunkView = (over: Partial<TrunkView> = {}, branches = ['A', 'B']) => {
    const view = layoutView();
    const trunk: TrunkView = {
      name: 'main',
      ref: 'origin/main',
      hash: 'abcdef1234567890abcdef1234567890abcdef12',
      time: 1_700_000_100,
      hasLocal: true,
      localAhead: 0,
      localBehind: 0,
      folder: '',
      test: '',
      releases: [],
      ...over,
    };
    view.groups = [{ trunk, branches }];
    return view;
  };
  // spies hands out one mock per action, so a test can look at its calls
  const spies = () => {
    const made: Record<string, ReturnType<typeof vi.fn>> = {};
    return new Proxy({} as Actions, { get: (_, k: string) => (made[k] ??= vi.fn()) });
  };
  const show = (view: View, a: Actions = spies(), locale = 'en') =>
    render(<Details host={host} t={makeT(locale)} view={view} actions={a} state={state({ selectedTrunk: 'main' })} />);
  // dd is the value next to a label of the details grid
  const dd = (label: string) => screen.getByText(label, { selector: 'dt' }).nextElementSibling!.textContent;

  it('shows the name, the ref it is compared through, its latest commit and how many branches it has', () => {
    const { container } = show(trunkView());
    expect(container.querySelector('.detail-name strong')!.textContent).toBe('main');
    expect(dd('Compared through')).toBe('origin/main');
    expect(dd('Latest commit of origin/main')).toContain('abcdef1');
    expect(dd('Latest commit of origin/main')).toContain(dateTime(1_700_000_100));
    expect(dd('Branches under it')).toBe('2');
    expect(screen.queryByText('Folder', { selector: 'dt' })).toBeNull();
  });

  it('says how the local branch stands against the remote', () => {
    const cases: Array<[Partial<TrunkView>, string]> = [
      [{ hasLocal: false }, 'None; only on the remote'],
      [{ ref: 'main' }, 'Local only; nothing on the remote to compare with'],
      [{}, 'Same as the remote'],
      [{ localAhead: 2 }, 'Ahead of the remote by 2'],
      [{ localBehind: 3 }, 'Behind the remote by 3'],
      [{ localAhead: 1, localBehind: 4 }, 'Ahead of the remote by 1 · Behind the remote by 4'],
    ];
    for (const [over, text] of cases) {
      show(trunkView(over));
      expect(dd('Local branch'), text).toBe(text);
      cleanup();
    }
  });

  it('names the local branch as what it is compared through when the remote has none', () => {
    show(trunkView({ ref: 'main' }));
    expect(dd('Compared through')).toBe('main (the local branch; the remote has none)');
    expect(dd('Latest commit of main')).toContain('abcdef1');
  });

  it('shows a configured test or release branch that no longer exists as not found, not as not set', () => {
    const view = trunkView({ test: '', releases: ['release/1.0'] });
    view.settings.trunks = [{ name: 'main', test: 'test', releases: ['release/1.0', 'release/0.9'] }];
    show(view);
    expect(dd('Test branch')).toBe('test (branch not found)');
    expect(dd('Release branches')).toBe('release/1.0, release/0.9 (branch not found)');
    expect(screen.queryByText(/Settings → Branch roles/)).toBeNull();
    cleanup();
    const found = trunkView({ test: 'test', releases: ['release/1.0'] });
    found.settings.trunks = [{ name: 'main', test: 'test', releases: ['release/1.0'] }];
    show(found);
    expect(dd('Test branch')).toBe('test');
    expect(dd('Release branches')).toBe('release/1.0');
  });

  it('says not set, with the hint, only when the settings name no branch for the role', () => {
    const view = trunkView();
    view.settings.trunks = [{ name: 'main', test: '', releases: ['release/0.9'] }];
    show(view);
    expect(dd('Test branch')).toBe('Not set');
    expect(dd('Release branches')).toBe('release/0.9 (branch not found)');
    expect(screen.getByText('Set them in Settings → Branch roles')).toBeTruthy();
  });

  it('shows the test and release branches, or that they are not set and where to set them', () => {
    show(trunkView({ test: 'test', releases: ['release/1.0', 'release/2.0'] }));
    expect(dd('Test branch')).toBe('test');
    expect(dd('Release branches')).toBe('release/1.0, release/2.0');
    expect(screen.queryByText(/Settings → Branch roles/)).toBeNull();
    cleanup();
    show(trunkView());
    expect(dd('Test branch')).toBe('Not set');
    expect(dd('Release branches')).toBe('Not set');
    expect(screen.getByText('Set them in Settings → Branch roles')).toBeTruthy();
  });

  it('offers Open in editor only when a folder has the trunk checked out', () => {
    const a = spies();
    show(trunkView({ folder: '/w/repo' }), a);
    expect(dd('Folder')).toBe('/w/repo');
    fireEvent.click(screen.getByRole('button', { name: 'Open in editor' }));
    expect(a.openFolder).toHaveBeenCalledWith('/w/repo');
    cleanup();
    show(trunkView());
    expect(screen.queryByRole('button', { name: 'Open in editor' })).toBeNull();
  });

  it('copies the trunk name and opens Compare, and has no button that changes anything', () => {
    const a = spies();
    const { container } = show(trunkView(), a);
    fireEvent.click(screen.getByRole('button', { name: 'Copy branch name' }));
    expect(a.copy).toHaveBeenCalledWith('main');
    fireEvent.click(screen.getByRole('button', { name: 'Compare with…' }));
    expect(a.openDialog).toHaveBeenCalledWith('compare');
    const buttons = Array.from(container.querySelectorAll('button')).map((b) => b.textContent);
    expect(buttons).toEqual(['Copy branch name', 'Compare with…', 'Recent commits']);
  });

  it('opens the recent commits of the trunk', () => {
    const a = spies();
    show(trunkView(), a);
    const recent = screen.getByRole('button', { name: 'Recent commits' });
    expect(recent.getAttribute('aria-pressed')).toBe('false');
    fireEvent.click(recent);
    expect(a.toggleNews).toHaveBeenCalledWith({ kind: 'trunk', trunk: 'main' });
  });

  it('speaks Chinese in a Chinese editor', () => {
    const zh = makeT('zh-cn');
    show(trunkView({ localBehind: 3 }), actions(), 'zh-cn');
    expect(dd(zh('trunk.local'))).toBe(zh('trunk.local.behind', { n: 3 }));
    expect(dd(zh('trunk.test'))).toBe(zh('trunk.notSet'));
  });

  it('shows a branch again once the trunk is no longer selected', () => {
    render(<Details host={host} t={t} view={trunkView()} actions={actions()} state={state({ selected: 'A' })} />);
    expect(screen.queryByText('Compared through')).toBeNull();
  });
});
