import { describe, expect, it } from 'vitest';
import { heightOf, laneX, rowGeometry, spansOf } from './graphGeometry';
import { layoutSection } from './graphFixture';
import { forkNewerSection } from './graphFixture';

describe('graph geometry, fork newer than the own commits', () => {
  const rows = forkNewerSection.rows;
  const spans = spansOf(rows);
  const geo = (i: number) => rowGeometry(rows, spans, i);

  it('starts lane 1 at the fork row', () => {
    expect(spans.find((s) => s.lane === 1)).toMatchObject({ start: 1, end: 3, forkFirst: true });
  });

  it('leaves the trunk dot at the fork row and meets the lane line', () => {
    const g = geo(1);
    const curve = g.curves.find((c) => c.lane === 1)!;
    const seg = g.segs.find((s) => s.lane === 1)!;
    const cy = heightOf('fork') / 2;
    expect(curve.from).toEqual({ x: 22, y: cy }); // the trunk dot
    expect(curve.to).toEqual({ x: laneX(1), y: cy });
    expect(seg).toMatchObject({ x: curve.to.x, y1: curve.to.y, y2: heightOf('fork') });
  });

  it('keeps lane 1 connected down to its last commit, with no stub above the fork row', () => {
    expect(geo(1).segs.filter((s) => s.lane === 1 && s.y1 === 0)).toHaveLength(0);
    const l1 = (i: number) => geo(i).segs.find((s) => s.lane === 1)!;
    expect(l1(2)).toMatchObject({ y1: 0, y2: heightOf('commit') }); // passes through B's commit row
    expect(l1(3)).toMatchObject({ y1: 0, y2: heightOf('commit') / 2 }); // ends at A's dot
  });

  it('joins lane 2 into the trunk at the last fork row without a gap', () => {
    const g = geo(4);
    const curve = g.curves.find((c) => c.lane === 2)!;
    expect(curve.from).toEqual({ x: laneX(2), y: 0 });
    expect(curve.to).toEqual({ x: 22, y: heightOf('fork') / 2 });
    expect(g.segs.filter((s) => s.lane === 2)).toHaveLength(0); // the curve replaces the line
    const above = geo(3).segs.find((s) => s.lane === 2)!;
    expect(above.y2).toBe(heightOf('commit'));
  });

  it('draws every lane stretch from the previous row without gaps', () => {
    for (const s of spans) {
      for (let i = s.start; i < s.end; i++) {
        const here = geo(i);
        const next = geo(i + 1);
        const bottom = here.segs.find((x) => x.lane === s.lane);
        const top = next.segs.find((x) => x.lane === s.lane) ?? null;
        if (bottom) expect(bottom.y2).toBe(here.height);
        if (top) expect(top.y1).toBe(0);
        else expect(next.curves.some((c) => c.lane === s.lane && c.from.y === 0)).toBe(true);
      }
    }
  });
});

describe('graph geometry, the layout fixture', () => {
  const rows = layoutSection.rows;
  const spans = spansOf(rows);

  it('closes a branch with a curve at its fork row, as before', () => {
    const g = rowGeometry(rows, spans, 7);
    expect(g.curves.map((c) => [c.lane, c.from.y])).toEqual([[1, 0], [2, 0]]);
    expect(g.segs.filter((s) => s.lane === 1 || s.lane === 2)).toHaveLength(0);
  });

  it('reuses lane 1 for the merged branch after A is closed', () => {
    expect(spans.filter((s) => s.lane === 1).map((s) => [s.branch, s.start, s.end])).toEqual([
      ['A', 1, 7],
      ['D', 8, 8],
    ]);
  });

  it('dashes the trunk in more rows and the branch in its more-own row', () => {
    expect(rowGeometry(rows, spans, 6).segs.find((s) => s.lane === 0)!.dashed).toBe(true);
    expect(rowGeometry(rows, spans, 5).segs.find((s) => s.lane === 3)!.dashed).toBe(true);
    expect(rowGeometry(rows, spans, 4).segs.find((s) => s.lane === 3)!.dashed).toBe(false);
  });
});
