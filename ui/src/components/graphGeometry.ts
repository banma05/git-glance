import type { Row } from '../types';

// Pure geometry of the branch graph: which lane segments and curves each row
// draws. No DOM, no colors; the component only paints what comes out of here.

export const LANE_X = 22; // at the wide spacing the trunk is lane 0 at 22; lane n sits at 22 + n * 22
export const LANE_STEP = 22;
export const LANE_FLOOR = 8; // the narrowest lane spacing: dots and lines still apart
export const GUTTER_SHARE = 0.4; // the lanes take at most this part of a row, down to the floor
export const NARROW_ROW = 480; // below this row width the lanes always take the floor
export const ROW_H = 28; // every graph row is one line of this height
export const EXPANDED_H = 22; // one hidden trunk commit

// Lanes is where the lanes of a section sit: the trunk (lane 0) at x0, then one
// lane every step.
export interface Lanes {
  x0: number;
  step: number;
}

export const WIDE: Lanes = { x0: LANE_X, step: LANE_STEP };

// laneLayout spaces the lanes of a section for a row `available` px wide. On a
// row narrower than NARROW_ROW (a sidebar) the lanes always take LANE_FLOOR:
// there every pixel the lanes do not need is a pixel of branch name, and all
// sections of the graph then leave their names the same room. On a wider row
// the wide spacing stays when it fits in GUTTER_SHARE of the row, else the
// lanes get closer until they fit, down to LANE_FLOOR. width is the gutter:
// the lanes and one step of margin on each side. An unknown width (0) keeps
// the wide spacing.
export function laneLayout(available: number, lanes: number): Lanes & { width: number } {
  const slots = Math.max(1, lanes) + 1;
  let step = LANE_STEP;
  if (available > 0 && available < NARROW_ROW) step = LANE_FLOOR;
  else if (available > 0 && slots * step > available * GUTTER_SHARE) {
    step = Math.max(LANE_FLOOR, Math.floor((available * GUTTER_SHARE) / slots));
  }
  return { x0: step, step, width: step * slots };
}

// NodeSizes are the radii of the marks on a lane, scaled so that a mark never
// reaches the next lane's line. hit is the clickable circle around a node.
export interface NodeSizes {
  dot: number; // a commit
  head: number; // the newest trunk commit
  hollow: number; // a fork point
  faint: number; // a merged branch
  small: number; // a hidden trunk commit in an opened list
  hit: number;
}

export function nodeSizes(step: number): NodeSizes {
  if (step >= 14) return { dot: 4, head: 5, hollow: 4, faint: 3, small: 2.5, hit: 10 };
  return { dot: 3, head: 3.5, hollow: 3, faint: 2.5, small: 2, hit: 10 };
}

export const laneX = (lane: number, l: Lanes = WIDE) => l.x0 + lane * l.step;
export const heightOf = (_kind: Row['kind']) => ROW_H;

// bend is how far a curve's control point sits from the trunk: 8 px at the
// wide spacing, closer when the lanes are closer.
export const bend = (l: Lanes) => (8 * Math.min(l.step, LANE_STEP)) / LANE_STEP;

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
// running. l spaces the lanes (see laneLayout).
export function rowGeometry(rows: Row[], spans: Span[], i: number, below = false, belowHeight = 0, l: Lanes = WIDE): RowGeometry {
  const r = rows[i];
  const h = below ? belowHeight : heightOf(r.kind);
  const cy = h / 2;
  const segs: Seg[] = [];
  const curves: Curve[] = [];
  const add = (lane: number, branch: string, top: boolean, bottom: boolean, dashed: boolean) => {
    if (!top && !bottom) return;
    segs.push({ lane, branch, x: laneX(lane, l), y1: top ? 0 : cy, y2: bottom ? h : cy, dashed });
  };
  if (rows.length > 1) add(0, '', below || i > 0, below || i < rows.length - 1, !below && r.kind === 'more');
  for (const s of spans) {
    if (i < s.start || i > s.end || (below && i >= s.end)) continue;
    const x = laneX(s.lane, l);
    const x0 = l.x0;
    const k = bend(l);
    const joins = !below && r.kind === 'fork' && r.ends.includes(s.lane);
    if (joins && s.forkFirst && s.start === i) {
      // the branch leaves the trunk dot here and runs down to its own commits
      const d = `M ${x0} ${cy} C ${x0 + k} ${cy}, ${x - k} ${cy}, ${x} ${cy}`;
      curves.push({ lane: s.lane, branch: s.branch, d, from: { x: x0, y: cy }, to: { x, y: cy } });
      add(s.lane, s.branch, false, true, false);
    } else if (joins && s.end === i) {
      // the branch comes down its lane and joins the trunk dot
      const d = `M ${x} 0 C ${x} ${cy}, ${x0 + k} ${cy}, ${x0} ${cy}`;
      curves.push({ lane: s.lane, branch: s.branch, d, from: { x, y: 0 }, to: { x: x0, y: cy } });
    } else {
      add(s.lane, s.branch, below || i > s.start, below || i < s.end, !below && r.kind === 'more-own' && s.branch === r.branch);
    }
  }
  return { height: h, segs, curves };
}
