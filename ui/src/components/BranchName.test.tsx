import * as fs from 'node:fs';
import * as path from 'node:path';
import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/preact';
import { BranchName } from './BranchName';
import { GraphView } from './GraphView';
import { ListView } from './ListView';
import { layoutSection, layoutView } from './graphFixture';
import { makeT } from '../i18n';
import { mockView } from '../host/mock';
import type { Host } from '../host/types';
import type { Actions } from '../state';

afterEach(cleanup);

const LONG = 'platform-migration/checkout-summary-redesign';
// visible is the text BranchName shows: the folders, then the last segment.
const visible = (el: Element) => (el.querySelector('.bname-pre')?.textContent ?? '') + el.querySelector('.bname-last')!.textContent;
const parts = (el: Element) => ({
  pre: el.querySelector('.bname-pre')?.textContent ?? null,
  last: el.querySelector('.bname-last')!.textContent,
});

describe('BranchName', () => {
  it('splits the name before its last segment, which keeps its "/"', () => {
    const { container } = render(<BranchName name={LONG} class="g-name" />);
    const el = container.firstElementChild!;
    expect(el.className).toBe('bname g-name');
    expect(parts(el)).toEqual({ pre: 'platform-migration', last: '/checkout-summary-redesign' });
    expect(visible(el)).toBe(LONG);
  });

  it('gives assistive technology the full name, whatever part is shown', () => {
    const { container } = render(<BranchName name={LONG} full={'alice/' + LONG} />);
    const el = container.firstElementChild!;
    expect(el.querySelector('.bname-pre')!.getAttribute('aria-hidden')).toBe('true');
    expect(el.querySelector('.bname-last')!.getAttribute('aria-hidden')).toBe('true');
    expect(el.querySelector('.vh')!.textContent).toBe('alice/' + LONG);
    cleanup();
    const plain = render(<BranchName name="main" />).container;
    expect(plain.querySelector('.vh')!.textContent).toBe('main'); // the shown name when no full name is given
  });

  it('has only the last part for a name without "/"', () => {
    const { container } = render(<BranchName name="main" />);
    expect(parts(container.firstElementChild!)).toEqual({ pre: null, last: 'main' });
  });

  it('keeps every folder of a deep name in the part that shrinks', () => {
    const { container } = render(<BranchName name="alice/feature/login" />);
    expect(parts(container.firstElementChild!)).toEqual({ pre: 'alice/feature', last: '/login' });
  });

  it('puts the last segment first in the DOM, so in its rtl box the folders are what wraps away', () => {
    const { container } = render(<BranchName name={LONG} />);
    const kids = [...container.firstElementChild!.children].map((x) => x.className);
    expect(kids).toEqual(['bname-last', 'bname-pre', 'vh']);
  });

  it('isolates the folders, so bidi never reorders them', () => {
    const { container } = render(<BranchName name={LONG} />);
    expect(container.querySelector('.bname-pre > bdi')!.textContent).toBe('platform-migration');
  });
});

describe('the rows that show branch names cut them from the front', () => {
  const t = makeT('en');
  const actions = new Proxy({} as Actions, { get: () => () => undefined });

  it('in the graph', () => {
    const view = layoutView(layoutSection);
    view.branches = { ...view.branches, A: { ...view.branches.A, name: 'team/' + LONG, short: LONG } };
    const { container } = render(
      <GraphView
        t={t}
        host={{} as Host}
        repo="/w/repo"
        view={view}
        theme="dark"
        selected=""
        onSelect={() => undefined}
        onCommit={() => undefined}
        onTrunk={() => undefined}
      />,
    );
    const row = container.querySelectorAll('.g-row')[1];
    expect(parts(row.querySelector('.bname')!)).toEqual({ pre: 'platform-migration', last: '/checkout-summary-redesign' });
    expect(row.getAttribute('title')).toContain('team/' + LONG); // the full name stays in the tooltip
    // and in the accessible name, also when the folders are dropped from sight
    expect(screen.getByRole('button', { name: new RegExp('^team/' + LONG) })).toBe(row);
  });

  for (const compact of [false, true]) {
    it(compact ? 'in the list beside the graph' : 'in the list', () => {
      const view = mockView('/demo/repo');
      const { container } = render(
        <ListView t={t} view={view} state={{ selected: '', collapsed: {}, theme: 'dark' }} actions={actions} compact={compact} />,
      );
      const row = [...container.querySelectorAll('.row')].find((r) => r.getAttribute('title') === 'alice/feature/login')!;
      expect(parts(row.querySelector('.bname')!)).toEqual({ pre: 'feature', last: '/login' });
      expect(screen.getByRole('button', { name: /^alice\/feature\/login/ })).toBe(row);
    });
  }
});

describe('the CSS that cuts names from the front', () => {
  const css = fs.readFileSync(path.resolve(__dirname, '../app.css'), 'utf8');
  const rule = (selector: string) => {
    const at = css.indexOf(`\n${selector} {`);
    expect(at, selector).toBeGreaterThanOrEqual(0);
    return css.slice(at, css.indexOf('}', at));
  };

  it('never hides the folders by row width: a name that fits is always whole', () => {
    expect(css).not.toMatch(/@container/);
    expect(css).not.toMatch(/\.bname-pre\s*\{[^}]*display:\s*none/);
  });

  it('drops the folders whole when the whole name does not fit: they wrap to a second line that is never shown', () => {
    const box = rule('.bname');
    expect(box).toMatch(/flex-wrap:\s*wrap/);
    expect(box).toMatch(/height:\s*1lh/);
    expect(box).toMatch(/overflow:\s*hidden/);
    expect(box).toMatch(/direction:\s*rtl/); // the last segment is placed first, so the folders are what wraps
    const pre = rule('.bname-pre');
    expect(pre).toMatch(/flex:\s*0 0 auto/); // whole or not at all: never a sliver
    expect(pre).not.toMatch(/text-overflow/);
    expect(rule('.bname-pre > bdi')).toMatch(/unicode-bidi:\s*isolate/);
  });

  it('shows "…" in the place of dropped folders, under the folders when they are there', () => {
    const before = rule('.bname::before');
    expect(before).toMatch(/content:\s*'…' \/ ''/);
    expect(before).toMatch(/position:\s*absolute/);
    expect(rule('.bname')).toMatch(/padding-left:\s*1em/);
    const pre = rule('.bname-pre');
    expect(pre).toMatch(/margin-left:\s*-1em/);
    expect(pre).toMatch(/min-width:\s*1em/);
    expect(pre).toMatch(/background:\s*var\(--row-bg, var\(--bg\)\)/);
    expect(pre).toMatch(/z-index:\s*1/);
    expect(rule('.row:hover, .row.selected')).toMatch(/--row-bg:\s*var\(--surface\)/);
    expect(rule('.g-row:hover, .g-row.selected')).toMatch(/--row-bg:\s*var\(--surface\)/);
  });

  it('cuts the last segment at its end only once the folders are gone', () => {
    const last = rule('.bname-last');
    expect(last).toMatch(/flex:\s*1 1 auto/);
    expect(last).toMatch(/min-width:\s*0/);
    expect(last).toMatch(/text-overflow:\s*ellipsis/);
  });

  it('in a graph row, lets the status chip go first, then the folders, then the last segment', () => {
    expect(rule('.g-text > .bname')).toMatch(/flex:\s*0 1 auto/);
    const chip = rule('.g-text > .chip');
    expect(chip).toMatch(/flex:\s*0 1e12 auto/);
    expect(chip).toMatch(/min-width:\s*[\d.]+em/); // a few characters of the status stay
    expect(chip).toMatch(/text-overflow:\s*ellipsis/);
  });

  it('hides the full name from sight but not from assistive technology', () => {
    const vh = rule('.vh');
    expect(vh).toMatch(/position:\s*absolute/);
    expect(vh).toMatch(/clip:\s*rect\(0 0 0 0\)/);
  });
});
