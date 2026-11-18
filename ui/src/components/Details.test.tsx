import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/preact';
import { Details } from './Details';
import { layoutView } from './graphFixture';
import { makeT } from '../i18n';
import type { Actions, AppState } from '../state';

const t = makeT('en');
const actions = () => new Proxy({} as Actions, { get: () => vi.fn() });
const state = (over: Partial<AppState>) => ({ selected: '', pick: null, busy: false, switchResult: null, ...over }) as AppState;

afterEach(cleanup);

describe('Details', () => {
  it('shows a trunk commit with the branches that start there', () => {
    const view = layoutView();
    view.branches.A.forkHash = 'F1';
    view.branches.B.forkHash = 'F1';
    render(
      <Details t={t} view={view} actions={actions()} state={state({ pick: { kind: 'trunk', trunk: 'main', hash: 'F1', time: 1_700_000_090, subject: 'Fix it' } })} />,
    );
    expect(screen.getByText('F1')).toBeTruthy();
    expect(screen.getByText('Fix it')).toBeTruthy();
    expect(screen.getByText('A')).toBeTruthy();
    expect(screen.getByText('B')).toBeTruthy();
    expect(screen.queryByText('C')).toBeNull();
  });

  it('says so when no branch starts at a trunk commit', () => {
    render(<Details t={t} view={layoutView()} actions={actions()} state={state({ pick: { kind: 'trunk', trunk: 'main', hash: 'H', time: 0, subject: '' } })} />);
    expect(screen.getByText('No branch starts here')).toBeTruthy();
  });

  it('shows the picked commit of a branch above the branch details', () => {
    const view = layoutView();
    view.branches.A.test = { state: 'some', matched: 1, total: 2, per: [{ hash: 'a2', subject: 'commit a2', time: 1_700_000_099, by: 'patch' }, { hash: 'a1', subject: 'commit a1', time: 1_700_000_098, by: '' }] };
    const { container } = render(<Details t={t} view={view} actions={actions()} state={state({ selected: 'A', pick: { kind: 'commit', branch: 'A', hash: 'a2' } })} />);
    expect(container.querySelector('.detail-commit code')!.textContent).toBe('a2');
    expect(container.querySelector('.detail-commit')!.textContent).toContain('same change');
    expect(screen.getAllByText('commit a2').length).toBeGreaterThan(0);
    expect(container.querySelector('.detail-name strong')!.textContent).toBe('A');
  });
});
