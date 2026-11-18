import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/preact';
import { GraphView } from './GraphView';
import { layoutSection, layoutView } from './graphFixture';
import { makeT } from '../i18n';
import { HostError } from '../host/types';
import type { Host } from '../host/types';
import type { Section } from '../types';

function fakeHost(trunkCommits: () => Promise<unknown>) {
  const calls: Array<{ method: string; params: Record<string, unknown> }> = [];
  const host = {
    async call(method: string, params: Record<string, unknown> = {}) {
      calls.push({ method, params });
      return trunkCommits();
    },
  } as unknown as Host;
  return { host, calls };
}

const commits = [
  { hash: 'x1', subject: 'Other one', time: 1_700_000_091 },
  { hash: 'x2', subject: 'Other two', time: 1_700_000_092 },
];

function setup(section: Section = layoutSection, trunkCommits: () => Promise<unknown> = async () => commits) {
  const { host, calls } = fakeHost(trunkCommits);
  const onSelect = vi.fn();
  const t = makeT('en');
  const utils = render(
    <GraphView t={t} host={host} repo="/w/repo" view={layoutView(section)} theme="dark" selected="" onSelect={onSelect} />,
  );
  return { ...utils, calls, onSelect };
}

const rows = (c: Element) => Array.from(c.querySelectorAll<HTMLElement>('.g-row'));

afterEach(cleanup);

describe('GraphView', () => {
  it('renders one row per layout row, with the dots on their lanes', () => {
    const { container } = setup();
    const r = rows(container);
    expect(r).toHaveLength(11);
    expect(r.map((x) => x.dataset.kind)).toEqual([
      'head', 'commit', 'commit', 'commit', 'commit', 'more-own', 'more', 'fork', 'merge', 'more', 'fork',
    ]);
    expect(r[1].querySelector('circle')!.getAttribute('cx')).toBe('44');
    expect(r[4].querySelector('circle')!.getAttribute('cx')).toBe('88');
    expect(r[0].querySelector('circle')!.getAttribute('cx')).toBe('22');
  });

  it('uses 34px rows and 26px rows for more, more-own and fork', () => {
    const { container } = setup();
    const h = rows(container).map((x) => x.style.height);
    expect(h).toEqual(['34px', '34px', '34px', '34px', '34px', '26px', '26px', '26px', '34px', '26px', '26px']);
  });

  it('draws the merge row as a dashed semi-transparent hook', () => {
    const { container } = setup();
    const hook = rows(container)[8].querySelector('path[stroke-dasharray]');
    expect(hook).toBeTruthy();
    expect(Number(hook!.getAttribute('opacity'))).toBeLessThan(1);
  });

  it('shows the commit count of a merged branch, and no 0 when the count is 0', () => {
    const first = setup();
    expect(rows(first.container)[8].textContent).toContain('2 commits');
    cleanup();
    const zero: Section = { ...layoutSection, rows: layoutSection.rows.map((r) => (r.kind === 'merge' ? { ...r, count: 0 } : r)) };
    const { container } = setup(zero);
    const text = rows(container)[8].textContent ?? '';
    expect(text).toContain('D');
    expect(text).not.toMatch(/0/);
  });

  it('shows the short name on line one, folder on line two, and the date as MM-DD', () => {
    const { container } = setup();
    const a = rows(container)[1];
    expect(a.querySelector('.g-name')!.textContent).toBe('A');
    expect(a.querySelector('.g-name')!.getAttribute('title')).toBe('A');
    expect(a.querySelector('.g-sub')!.textContent).toContain('wt-a');
    expect(a.querySelector('.g-date')!.textContent).toMatch(/^\d\d-\d\d$/);
  });

  it('selects a branch when its row is clicked', () => {
    const { container, onSelect } = setup();
    fireEvent.click(rows(container)[3].querySelector('button')!);
    expect(onSelect).toHaveBeenCalledWith('B');
  });

  it('draws the more-own row dim and italic with a dashed run', () => {
    const { container } = setup();
    const more = rows(container)[5];
    expect(more.querySelector('.g-text')!.className).toContain('dim');
    expect(more.querySelector('[stroke-dasharray]')).toBeTruthy();
    expect(more.textContent).toContain('4 more commits');
  });

  it('expands the hidden trunk commits in place and collapses them on a second click', async () => {
    const { container, calls } = setup();
    fireEvent.click(rows(container)[6].querySelector('button')!);
    expect(await screen.findByText('Other one')).toBeTruthy();
    expect(calls).toHaveLength(1);
    expect(calls[0].method).toBe('trunkCommits');
    expect(calls[0].params).toMatchObject({ repo: '/w/repo', newer: 'H', older: 'F1' });
    fireEvent.click(rows(container)[6].querySelector('button')!);
    expect(screen.queryByText('Other one')).toBeNull();
    expect(calls).toHaveLength(1);
  });

  it('uses the fork above and the fork below as anchors for the second more row', async () => {
    const { container, calls } = setup();
    fireEvent.click(rows(container)[9].querySelector('button')!);
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0].params).toMatchObject({ newer: 'F1', older: 'F2' });
  });

  it('sends one request at a time per row', async () => {
    let release: (v: unknown) => void = () => undefined;
    const pending = new Promise((r) => (release = r));
    const { container, calls } = setup(layoutSection, () => pending);
    const btn = rows(container)[6].querySelector('button')!;
    fireEvent.click(btn);
    fireEvent.click(btn);
    expect(calls).toHaveLength(1);
    release(commits);
    expect(await screen.findByText('Other one')).toBeTruthy();
  });

  it('shows the error with a retry that asks again', async () => {
    let n = 0;
    const { container, calls } = setup(layoutSection, async () => {
      n++;
      if (n === 1) throw new HostError('git-failed', 'boom');
      return commits;
    });
    fireEvent.click(rows(container)[6].querySelector('button')!);
    expect(await screen.findByText(/Could not load/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(await screen.findByText('Other one')).toBeTruthy();
    expect(calls).toHaveLength(2);
  });

  it('says so when fewer commits came back than the row counts', async () => {
    const { container } = setup(layoutSection, async () => commits);
    fireEvent.click(rows(container)[6].querySelector('button')!);
    expect(await screen.findByText('Showing 2 of 6')).toBeTruthy();
  });
});
