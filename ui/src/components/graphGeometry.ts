import type { Row } from '../types';

// Pure geometry of the branch graph: which lane segments and curves each row
// draws. No DOM, no colors; the component only paints what comes out of here.

export const LANE_X = 22; // the trunk is lane 0; lane n sits at 22 + n * 22
export const LANE_STEP = 22;
export const ROW_H = 28; // every graph row is one line of this height
export const EXPANDED_H = 22; // one hidden trunk commit

export const laneX = (lane: number) => LANE_X + lane * LANE_STEP;
export const heightOf = (_kind: Row['kind']) => ROW_H;

// A Span is the stretch of one lane that belongs to one branch: from its first
// row to its last row (the row where it joins the trunk, or its last commit).
export interface Span {
  lane: number;
  branch: string; // '' until a commit row names it
  start: number;
  end: number;
  forkFirst: boolean; // opened by a fork row that sorts above the branch's commits
}

export function spansOf(rows: Row[]): Span[] {
  const open = new Map<number, Span>();
  const out: Span[] = [];
  const reach = (lane: number, i: number, branch: string, byFork: boolean): Span => {
    let s = open.get(lane);
    if (s && byFork && s.forkFirst) s = undefined; // that fork is already behind us: a new branch uses the lane
    if (s && !byFork && branch && s.branch && s.branch !== branch) s = undefined;
    if (!s) {
      s = { lane, branch, start: i, end: i, forkFirst: byFork };
      open.set(lane, s);
      out.push(s);
    } else {
      s.end = i;
      if (!s.branch) s.branch = branch;
    }
    return s;
  };
  rows.forEach((r, i) => {
    if (r.kind === 'commit' || r.kind === 'more-own' || r.kind === 'merge') {
      reach(r.lane, i, r.branch, false);
    } else if (r.kind === 'fork') {
      for (const lane of r.ends) {
        const s = reach(lane, i, '', true);
        if (!s.forkFirst) open.delete(lane); // the branch joins the trunk here
      }
    }
  });
  return out;
}

// Seg is one vertical piece of a lane inside a row (y from the row's top).
export interface Seg {
  lane: number;
  branch: string; // '' for the trunk
  x: number;
  y1: number;
  y2: number;
  dashed: boolean;
}

// Curve joins a branch lane and the trunk dot of a fork row. `from` is where the
// path starts, `to` where it ends.
export interface Curve {
  lane: number;
  branch: string;
  d: string;
  from: { x: number; y: number };
  to: { x: number; y: number };
}

export interface RowGeometry {
  height: number;
  segs: Seg[];
  curves: Curve[];
}

// rowGeometry works out row i. With below=true it is the stretch just under the
// row (inside an expanded list, belowHeight tall), where every open line keeps
// running.
export function rowGeometry(rows: Row[], spans: Span[], i: number, below = false, belowHeight = 0): RowGeometry {
  const r = rows[i];
  const h = below ? belowHeight : heightOf(r.kind);
  const cy = h / 2;
  const segs: Seg[] = [];
  const curves: Curve[] = [];
  const add = (lane: number, branch: string, top: boolean, bottom: boolean, dashed: boolean) => {
    if (!top && !bottom) return;
    segs.push({ lane, branch, x: laneX(lane), y1: top ? 0 : cy, y2: bottom ? h : cy, dashed });
  };
  if (rows.length > 1) add(0, '', below || i > 0, below || i < rows.length - 1, !below && r.kind === 'more');
  for (const s of spans) {
    if (i < s.start || i > s.end || (below && i >= s.end)) continue;
    const x = laneX(s.lane);
    const joins = !below && r.kind === 'fork' && r.ends.includes(s.lane);
    if (joins && s.forkFirst && s.start === i) {
      // the branch leaves the trunk dot here and runs down to its own commits
      const d = `M ${LANE_X} ${cy} C ${LANE_X + 8} ${cy}, ${x - 8} ${cy}, ${x} ${cy}`;
      curves.push({ lane: s.lane, branch: s.branch, d, from: { x: LANE_X, y: cy }, to: { x, y: cy } });
      add(s.lane, s.branch, false, true, false);
    } else if (joins && s.end === i) {
      // the branch comes down its lane and joins the trunk dot
      const d = `M ${x} 0 C ${x} ${cy}, ${LANE_X + 8} ${cy}, ${LANE_X} ${cy}`;
      curves.push({ lane: s.lane, branch: s.branch, d, from: { x, y: 0 }, to: { x: LANE_X, y: cy } });
    } else {
      add(s.lane, s.branch, below || i > s.start, below || i < s.end, !below && r.kind === 'more-own' && s.branch === r.branch);
    }
  }
  return { height: h, segs, curves };
}
