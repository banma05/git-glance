import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/preact';
import { GraphView } from './GraphView';
import { layoutSection, layoutView } from './graphFixture';
import { makeT } from '../i18n';
import { HostError } from '../host/types';
import type { Host } from '../host/types';
import { PALETTE } from '../palette';
import type { Picked } from '../state';
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

interface Opts {
  section?: Section;
  trunkCommits?: () => Promise<unknown>;
  selected?: string;
  pick?: Picked | null;
  inline?: preact.ComponentChildren;
}

function setup(o: Opts = {}) {
  const { host, calls } = fakeHost(o.trunkCommits ?? (async () => commits));
  const onSelect = vi.fn();
  const onCommit = vi.fn();
  const onTrunk = vi.fn();
  const utils = render(
    <GraphView
      t={makeT('en')}
      host={host}
      repo="/w/repo"
      view={layoutView(o.section ?? layoutSection)}
      theme="dark"
      selected={o.selected ?? ''}
      pick={o.pick ?? null}
      onSelect={onSelect}
      onCommit={onCommit}
      onTrunk={onTrunk}
      inline={o.inline}
    />,
  );
  return { ...utils, calls, onSelect, onCommit, onTrunk };
}

// text of the row without the tooltips inside its SVG
const words = (row: Element) => row.querySelector('.g-text')!.textContent;
const rows = (c: Element) => Array.from(c.querySelectorAll<HTMLElement>('.g-row:not(.g-expanded)'));

// the color a browser would paint for a hex value, in the same notation as the DOM gives back
function painted(hex: string): string {
  const probe = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
  probe.style.fill = hex;
  return probe.style.fill;
}

afterEach(cleanup);

describe('GraphView rows', () => {
  it('renders one row per layout row, with the dots on their lanes', () => {
    const { container } = setup();
    const r = rows(container);
    expect(r).toHaveLength(11);
    expect(r.map((x) => x.dataset.kind)).toEqual([
      'head', 'commit', 'commit', 'commit', 'commit', 'more-own', 'more', 'fork', 'merge', 'more', 'fork',
    ]);
    expect(r[1].querySelector('.g-dot')!.getAttribute('cx')).toBe('44');
    expect(r[4].querySelector('.g-dot')!.getAttribute('cx')).toBe('88');
    expect(r[0].querySelector('.g-dot')!.getAttribute('cx')).toBe('22');
  });

  it('uses one compact height for every row', () => {
    const { container } = setup();
    expect(new Set(rows(container).map((x) => x.style.height))).toEqual(new Set(['28px']));
  });

  it('has one line per row: the name and at most one chip, no second line, no date', () => {
    const { container } = setup();
    expect(container.querySelector('.g-sub')).toBeNull();
    expect(container.querySelector('.g-date')).toBeNull();
    for (const row of rows(container)) expect(row.querySelectorAll('.chip').length).toBeLessThanOrEqual(1);
    const a = rows(container)[1];
    expect(a.querySelector('.g-name')!.textContent).toBe('A');
    expect(a.getAttribute('title')).toContain('A');
    expect(a.textContent).not.toContain('wt-a');
  });

  it('shows the commit subject, dim, on the later rows of a branch', () => {
    const { container } = setup();
    const second = rows(container)[2];
    expect(words(second)).toBe('commit a1');
    expect(second.querySelector('.g-text')!.className).toContain('dim');
  });

  it('renders no text on a fork row', () => {
    const { container } = setup();
    for (const i of [7, 10]) {
      expect(words(rows(container)[i])).toBe('');
      expect(rows(container)[i].querySelector('.g-hollow')).toBeTruthy();
    }
  });

  it('draws the merge row as a dashed semi-transparent hook with just the name', () => {
    const { container } = setup();
    const merge = rows(container)[8];
    const hook = merge.querySelector('path[stroke-dasharray]');
    expect(hook).toBeTruthy();
    expect(Number(hook!.getAttribute('opacity'))).toBeLessThan(1);
    expect(words(merge)).toBe('D');
    expect(words(merge)).not.toMatch(/0/);
  });

  it('draws the more-own row dim and italic with a dashed run', () => {
    const more = rows(setup().container)[5];
    expect(more.querySelector('.g-text')!.className).toContain('dim');
    expect(more.querySelector('[stroke-dasharray]')).toBeTruthy();
    expect(more.textContent).toContain('4 more commits');
  });
});

describe('GraphView colors', () => {
  it('keeps the trunk lane and the trunk dots out of the palette', () => {
    const { container } = setup();
    const trunk = Array.from(container.querySelectorAll<SVGElement>('.g-trunk'));
    expect(trunk.length).toBeGreaterThan(0);
    for (const l of trunk) expect(l.getAttribute('style') ?? '').toBe('');
    for (const d of container.querySelectorAll<SVGElement>('[data-kind="head"] .g-dot, [data-kind="fork"] .g-dot')) {
      expect(d.getAttribute('style') ?? '').toBe('');
      expect(d.getAttribute('class')).toMatch(/g-trunk-dot|g-hollow/);
    }
  });

  it('gives each branch its own lane at x >= 44 in its palette color', () => {
    const { container } = setup();
    const view = layoutView();
    const dots = rows(container)
      .filter((r) => r.dataset.kind === 'commit')
      .map((r) => r.querySelector<SVGCircleElement>('.g-dot')!);
    const byLane = new Map<string, string>();
    dots.forEach((d, i) => {
      const name = ['A', 'A', 'B', 'C'][i];
      const x = Number(d.getAttribute('cx'));
      expect(x).toBeGreaterThanOrEqual(44);
      expect(d.style.fill).toBe(painted(PALETTE.dark[view.branches[name].color]));
      byLane.set(name, String(x));
    });
    expect(new Set(byLane.values()).size).toBe(3); // A, B and C on three different lanes
    const colored = Array.from(container.querySelectorAll<SVGLineElement>('line.g-line:not(.g-trunk)'));
    expect(colored.length).toBeGreaterThan(0);
    for (const l of colored) expect(Number(l.getAttribute('x1'))).toBeGreaterThanOrEqual(44);
  });
});

describe('GraphView clicks', () => {
  it('picks the tip commit when the named row of a branch is clicked (branch details plus that commit)', () => {
    const { container, onSelect, onCommit } = setup();
    fireEvent.click(rows(container)[3]);
    expect(onCommit).toHaveBeenCalledWith('B', 'b1');
    expect(onSelect).not.toHaveBeenCalled();
  });

  it('selects a commit when a later row of a branch is clicked', () => {
    const { container, onCommit, onSelect } = setup();
    fireEvent.click(rows(container)[2]);
    expect(onCommit).toHaveBeenCalledWith('A', 'a1');
    expect(onSelect).not.toHaveBeenCalled();
  });

  it('selects the branch of a merge row and of a more-own row', () => {
    const { container, onSelect } = setup();
    fireEvent.click(rows(container)[8]);
    fireEvent.click(rows(container)[5]);
    expect(onSelect.mock.calls).toEqual([['D'], ['C']]);
  });

  it('selects a trunk commit when a head or fork dot is clicked', () => {
    const { container, onTrunk } = setup();
    fireEvent.click(rows(container)[0].querySelector('.g-dot')!);
    fireEvent.click(rows(container)[7].querySelector('.g-hollow')!);
    expect(onTrunk.mock.calls[0][0]).toMatchObject({ trunk: 'main', hash: 'H' });
    expect(onTrunk.mock.calls[1][0]).toMatchObject({ trunk: 'main', hash: 'F1' });
  });

  it('makes the dot hit area at least 16px wide and selects through it', () => {
    const { container, onCommit } = setup();
    const hit = rows(container)[3].querySelector<SVGCircleElement>('.g-hit')!;
    expect(Number(hit.getAttribute('r')) * 2).toBeGreaterThanOrEqual(16);
    fireEvent.click(hit);
    expect(onCommit).toHaveBeenCalledWith('B', 'b1');
    const dotClick = setup();
    fireEvent.click(rows(dotClick.container)[3].querySelector('.g-dot')!);
    expect(dotClick.onCommit).toHaveBeenCalledWith('B', 'b1');
  });

  it('makes every row a focusable button, so Enter and Space work', () => {
    const { container } = setup();
    for (const row of rows(container)) {
      expect(row.tagName).toBe('BUTTON');
      expect(row.tabIndex).toBeGreaterThanOrEqual(0);
    }
    rows(container)[3].focus();
    expect(document.activeElement).toBe(rows(container)[3]);
  });

  it('shows the inline details under the row the selection is about', () => {
    const inline = <div class="probe">details</div>;
    const under = (c: Element) => c.querySelector('.probe')!.parentElement!.previousElementSibling;
    let r = setup({ selected: 'B', inline });
    expect(under(r.container)).toBe(rows(r.container)[3]);
    cleanup();
    r = setup({ selected: 'A', pick: { kind: 'commit', branch: 'A', hash: 'a1' }, inline });
    expect(under(r.container)).toBe(rows(r.container)[2]);
    cleanup();
    r = setup({ pick: { kind: 'trunk', trunk: 'main', hash: 'F1', time: 1, subject: '' }, inline });
    expect(under(r.container)).toBe(rows(r.container)[7]);
    cleanup();
    r = setup({ inline });
    expect(r.container.querySelector('.probe')).toBeNull();
  });

  it('keeps the inline details with the branch when a new view moves its row', () => {
    const inline = <div class="probe">details</div>;
    const { container, rerender } = setup({ selected: 'B', inline });
    const moved: Section = { ...layoutSection, rows: [layoutSection.rows[0], layoutSection.rows[3], ...layoutSection.rows.slice(1, 3), ...layoutSection.rows.slice(4)] };
    rerender(
      <GraphView
        t={makeT('en')}
        host={fakeHost(async () => commits).host}
        repo="/w/repo"
        view={layoutView(moved)}
        theme="dark"
        selected="B"
        pick={null}
        onSelect={() => undefined}
        onCommit={() => undefined}
        onTrunk={() => undefined}
        inline={inline}
      />,
    );
    const probe = container.querySelector('.probe')!;
    expect(probe.parentElement!.previousElementSibling).toBe(rows(container)[1]);
    expect(container.querySelectorAll('.probe')).toHaveLength(1);
  });

  it('shows the details of a hidden trunk commit under the opened list', async () => {
    const inline = <div class="probe">details</div>;
    const { container, rerender } = setup({ inline });
    fireEvent.click(rows(container)[6]);
    await screen.findByText('Other one');
    rerender(
      <GraphView
        t={makeT('en')}
        host={fakeHost(async () => commits).host}
        repo="/w/repo"
        view={layoutView(layoutSection)}
        theme="dark"
        selected=""
        pick={{ kind: 'trunk', trunk: 'main', hash: 'x2', time: 1, subject: 'Other two' }}
        onSelect={() => undefined}
        onCommit={() => undefined}
        onTrunk={() => undefined}
        inline={inline}
      />,
    );
    const probe = container.querySelector('.probe')!;
    const before = probe.parentElement!.previousElementSibling as HTMLElement;
    expect(before.className).toContain('g-expanded'); // right after the opened list
    const picked = screen.getByText('Other two').closest('button')!;
    expect(picked.compareDocumentPosition(probe) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(container.querySelectorAll('.probe')).toHaveLength(1);
  });
});

describe('GraphView hover and selection', () => {
  const lane = (c: Element, name: 'A' | 'B' | 'C') => {
    const x = { A: 44, B: 66, C: 88 }[name];
    return Array.from(c.querySelectorAll<SVGElement>('line.g-line')).filter((l) => Number(l.getAttribute('x1')) === x);
  };

  it('highlights the lane of the hovered branch and dims the others', () => {
    const { container } = setup();
    fireEvent.mouseEnter(rows(container)[3].querySelector('.g-dot')!.closest('button')!);
    const b = lane(container, 'B');
    expect(b.length).toBeGreaterThan(0);
    for (const l of b) expect(l.getAttribute('class')).toContain('g-hl');
    for (const l of [...lane(container, 'A'), ...lane(container, 'C')]) {
      expect(l.getAttribute('class')).not.toContain('g-hl');
      expect(l.getAttribute('class')).toContain('g-dim');
    }
    fireEvent.mouseLeave(rows(container)[3]);
    expect(container.querySelector('.g-hl')).toBeNull();
    expect(container.querySelector('.g-dim')).toBeNull();
  });

  it('fades the dots, hooks and names of the other branches while one is hovered', () => {
    const { container } = setup();
    fireEvent.mouseEnter(rows(container)[3]);
    const r = rows(container);
    for (const i of [1, 2, 4, 5, 8]) expect(r[i].className, `row ${i}`).toContain('g-faded');
    for (const i of [0, 3, 6, 7, 9, 10]) expect(r[i].className, `row ${i}`).not.toContain('g-faded');
    fireEvent.mouseLeave(r[3]);
    expect(container.querySelector('.g-faded')).toBeNull();
  });

  it('keeps the ring of the selected node visible under hover (CSS)', () => {
    const css = readFileSync(resolve(__dirname, '../app.css'), 'utf8');
    const hover = css.indexOf('.g-row:hover .g-dot');
    const ring = css.indexOf('.g-row:hover .g-dot.g-ring');
    expect(hover).toBeGreaterThanOrEqual(0);
    expect(ring).toBeGreaterThan(hover);
    expect(css.slice(ring, css.indexOf('}', ring))).toMatch(/stroke:\s*var\(--tone-info\)/);
    expect(css).toMatch(/\.g-dot\.g-ring\.g-hl/);
  });

  it('puts the branch name and its top chip in the tooltip of the dot and of the trunk dot', () => {
    const { container } = setup();
    expect(rows(container)[1].querySelector('.g-hit title')!.textContent).toBe('A · In sync with remote');
    expect(rows(container)[0].querySelector('.g-hit title')!.textContent).toBe('H · trunk');
  });

  it('rings the selected node and keeps its row highlighted', () => {
    const { container } = setup({ selected: 'B' });
    expect(rows(container)[3].querySelector('.g-dot')!.getAttribute('class')).toContain('g-ring');
    expect(rows(container)[3].className).toContain('selected');
    expect(container.querySelectorAll('.g-ring')).toHaveLength(1);
  });

  it('rings the picked commit instead of the branch row', () => {
    const { container } = setup({ selected: 'A', pick: { kind: 'commit', branch: 'A', hash: 'a1' } });
    expect(rows(container)[2].querySelector('.g-dot')!.getAttribute('class')).toContain('g-ring');
    expect(rows(container)[1].querySelector('.g-ring')).toBeNull();
  });

  it('rings the picked trunk commit', () => {
    const { container } = setup({ pick: { kind: 'trunk', trunk: 'main', hash: 'F1', time: 1, subject: '' } });
    expect(rows(container)[7].querySelector('.g-hollow')!.getAttribute('class')).toContain('g-ring');
    expect(container.querySelectorAll('.g-ring')).toHaveLength(1);
  });
});

describe('GraphView hidden trunk commits', () => {
  it('expands the hidden trunk commits in place and collapses them on a second click', async () => {
    const { container, calls } = setup();
    fireEvent.click(rows(container)[6]);
    expect(await screen.findByText('Other one')).toBeTruthy();
    expect(calls).toHaveLength(1);
    expect(calls[0].method).toBe('trunkCommits');
    expect(calls[0].params).toMatchObject({ repo: '/w/repo', newer: 'H', older: 'F1' });
    fireEvent.click(rows(container)[6]);
    expect(screen.queryByText('Other one')).toBeNull();
    expect(calls).toHaveLength(1);
  });

  it('uses the fork above and the fork below as anchors for the second more row', async () => {
    const { container, calls } = setup();
    fireEvent.click(rows(container)[9]);
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0].params).toMatchObject({ newer: 'F1', older: 'F2' });
  });

  it('sends one request at a time per row', async () => {
    let release: (v: unknown) => void = () => undefined;
    const pending = new Promise((r) => (release = r));
    const { container, calls } = setup({ trunkCommits: () => pending });
    fireEvent.click(rows(container)[6]);
    fireEvent.click(rows(container)[6]);
    expect(calls).toHaveLength(1);
    release(commits);
    expect(await screen.findByText('Other one')).toBeTruthy();
  });

  it('shows the error with a retry that asks again', async () => {
    let n = 0;
    const { container, calls } = setup({
      trunkCommits: async () => {
        n++;
        if (n === 1) throw new HostError('git-failed', 'boom');
        return commits;
      },
    });
    fireEvent.click(rows(container)[6]);
    expect(await screen.findByText(/Could not load/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(await screen.findByText('Other one')).toBeTruthy();
    expect(calls).toHaveLength(2);
  });

  it('says so when fewer commits came back than the row counts', async () => {
    const { container } = setup();
    fireEvent.click(rows(container)[6]);
    expect(await screen.findByText('Showing 2 of 6')).toBeTruthy();
  });

  it('selects an expanded trunk commit with its subject', async () => {
    const { container, onTrunk } = setup();
    fireEvent.click(rows(container)[6]);
    fireEvent.click((await screen.findByText('Other one')).closest('button')!);
    expect(onTrunk).toHaveBeenCalledWith({ trunk: 'main', hash: 'x1', time: 1_700_000_091, subject: 'Other one' });
  });
});
