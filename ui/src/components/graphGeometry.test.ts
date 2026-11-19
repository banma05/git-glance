import { describe, expect, it } from 'vitest';
import { LANE_FLOOR, LANE_STEP, WIDE, heightOf, laneLayout, laneX, nodeSizes, rowGeometry, spansOf } from './graphGeometry';
import { layoutSection } from './graphFixture';
import { forkNewerSection } from './graphFixture';

describe('lane spacing from the width', () => {
  // [row width, lanes, expected step]
  // Below 480 px (a sidebar) the lanes always take the floor: every pixel they
  // do not need is a pixel of branch name. The gutter then stays within 30%.
  const cases: Array<[number, number, number]> = [
    [1400, 2, 22], [1400, 8, 22], [1400, 14, 22],
    [600, 14, 16], [480, 8, 21], [480, 2, 22],
    [400, 4, 8], [360, 4, 8],
    [360, 2, 8], [360, 8, 8], [360, 14, 8],
    [300, 2, 8], [300, 8, 8], [300, 14, 8],
    [240, 2, 8], [240, 8, 8], [240, 14, 8],
  ];
  for (const [width, lanes, step] of cases) {
    it(`${width} px, ${lanes} lanes: step ${step}`, () => {
      const l = laneLayout(width, lanes);
      expect(l.step).toBe(step);
      expect(l.x0).toBe(step); // the trunk sits one step in
      expect(l.width).toBe(step * (lanes + 1));
      // the gutter takes at most 40% of the row, unless the floor stops it
      if (step > LANE_FLOOR) expect(l.width).toBeLessThanOrEqual(width * (width < 480 ? 0.3 : 0.4));
      expect(l.step).toBeGreaterThanOrEqual(LANE_FLOOR);
    });
  }

  it('keeps the wide spacing when the width is not known yet', () => {
    expect(laneLayout(0, 14)).toEqual({ ...WIDE, width: 22 * 15 });
    expect(WIDE).toEqual({ x0: 22, step: LANE_STEP });
  });

  it('puts every lane on its own x, the trunk first', () => {
    const l = laneLayout(600, 14);
    expect([0, 1, 7].map((n) => laneX(n, l))).toEqual([16, 32, 128]);
    expect(laneX(3)).toBe(22 + 3 * 22); // the wide spacing by default
  });

  it('keeps nodes inside their lane at every step: no dot reaches the next lane line', () => {
    for (let step = LANE_FLOOR; step <= LANE_STEP; step++) {
      const s = nodeSizes(step);
      for (const r of [s.dot, s.head, s.hollow, s.faint, s.small]) {
        expect(r + 1.5, `step ${step}`).toBeLessThan(step - 1); // radius, half the outline, half the next line
      }
      expect(s.hit).toBeGreaterThanOrEqual(8); // the hit circle stays at least 16 px wide
    }
    expect(nodeSizes(LANE_STEP)).toEqual({ dot: 4, head: 5, hollow: 4, faint: 3, small: 2.5, hit: 10 });
  });

  it('draws the same curves as before at the wide spacing, and keeps them right of the trunk when narrow', () => {
    const rows = layoutSection.rows;
    const spans = spansOf(rows);
    const wide = rowGeometry(rows, spans, 7);
    expect(wide.curves.find((c) => c.lane === 1)!.d).toBe('M 44 0 C 44 14, 30 14, 22 14');
    const l = laneLayout(240, layoutSection.lanes);
    const narrow = rowGeometry(rows, spans, 7, false, 0, l);
    const c = narrow.curves.find((x) => x.lane === 1)!;
    expect(c.from).toEqual({ x: laneX(1, l), y: 0 });
    expect(c.to).toEqual({ x: l.x0, y: 14 });
    const xs = [...c.d.matchAll(/(-?[\d.]+) (-?[\d.]+)/g)].map((m) => Number(m[1]));
    for (const x of xs) expect(x).toBeGreaterThanOrEqual(l.x0);
    for (const seg of narrow.segs) expect(seg.x).toBe(laneX(seg.lane, l));
  });
});

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

describe('graph geometry, heights and the trunk lane', () => {
  it('gives every kind the same compact row height', () => {
    const kinds = ['head', 'commit', 'more-own', 'fork', 'merge', 'more'] as const;
    expect(new Set(kinds.map((k) => heightOf(k))).size).toBe(1);
    expect(heightOf('commit')).toBe(28);
  });

  it('never names a branch on a trunk segment, and keeps every branch lane at x >= 44', () => {
    for (const section of [layoutSection, forkNewerSection]) {
      const spans = spansOf(section.rows);
      section.rows.forEach((_, i) => {
        for (const seg of rowGeometry(section.rows, spans, i).segs) {
          if (seg.lane === 0) expect(seg.branch).toBe('');
          else {
            expect(seg.branch).not.toBe('');
            expect(seg.x).toBeGreaterThanOrEqual(44);
          }
        }
      });
    }
  });
});
