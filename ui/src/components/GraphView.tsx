import type { ComponentChildren } from 'preact';
import { useRef, useState } from 'preact/hooks';
import { rpc } from '../api';
import { chips, topChip } from '../format';
import type { Host } from '../host/types';
import type { T } from '../i18n';
import { errorText } from '../reasons';
import type { Picked, TrunkPick } from '../state';
import type { BranchView, CommitInfo, Row, View } from '../types';
import { EXPANDED_H, LANE_STEP, LANE_X, heightOf, laneX, rowGeometry, spansOf } from './graphGeometry';
import type { Curve, Seg } from './graphGeometry';
import { branchColor } from './ListView';

const HIT_R = 10; // the clickable circle around a dot is 20px wide

interface GutterProps {
  width: number;
  height: number;
  segs: Seg[];
  curves?: Curve[];
  hover: string; // the branch under the mouse, '' when none
  colorOf(branch: string): string;
  children?: ComponentChildren;
}

// lineClass marks the lane of the hovered branch and fades the other branches.
function lineClass(branch: string, hover: string): string {
  let c = branch ? 'g-line' : 'g-line g-trunk';
  if (hover && branch) c += branch === hover ? ' g-hl' : ' g-dim';
  return c;
}

function Gutter({ width, height, segs, curves = [], hover, colorOf, children }: GutterProps) {
  return (
    <svg class="g-gutter" width={width} height={height} viewBox={`0 0 ${width} ${height}`} aria-hidden="true">
      {segs.map((l) => {
        const color = l.branch ? colorOf(l.branch) : '';
        return (
          <line
            class={lineClass(l.branch, hover)}
            style={color ? { stroke: color } : undefined}
            x1={l.x}
            x2={l.x}
            y1={l.y1}
            y2={l.y2}
            stroke-dasharray={l.dashed ? '3 3' : undefined}
          />
        );
      })}
      {curves.map((c) => (
        <path class={lineClass(c.branch, hover)} style={{ stroke: colorOf(c.branch) || undefined }} d={c.d} fill="none" />
      ))}
      {children}
    </svg>
  );
}

type Fetched = { status: 'loading' } | { status: 'ok'; commits: CommitInfo[] } | { status: 'error'; reason: string };

interface Props {
  t: T;
  host: Host;
  repo: string;
  view: View;
  theme: 'dark' | 'light';
  selected: string;
  pick?: Picked | null;
  onSelect(branch: string): void;
  onCommit(branch: string, hash: string): void;
  onTrunk(p: TrunkPick): void;
  inline?: ComponentChildren; // details shown right under the clicked row (the narrow layout)
}

// GraphView draws one block per trunk, one line per row. A row is one button:
// the lanes as a small SVG, then a name and at most one chip. The details of
// whatever was clicked live in the detail area (or under the row, see `inline`).
export function GraphView({ t, host, repo, view, theme, selected, pick = null, onSelect, onCommit, onTrunk, inline }: Props) {
  const fetched = useRef<Record<string, Fetched>>({});
  const [, redraw] = useState(0);
  const [hover, setHover] = useState('');
  const [anchor, setAnchor] = useState(''); // the row the user clicked last
  const put = (key: string, value: Fetched | undefined) => {
    if (value) fetched.current[key] = value;
    else delete fetched.current[key];
    redraw((n) => n + 1);
  };

  // One request per row at a time; an open list closes on the next click, a
  // failed one asks again.
  const toggle = (key: string, newer: string, older: string) => {
    const cur = fetched.current[key];
    if (cur?.status === 'loading') return;
    if (cur?.status === 'ok') {
      put(key, undefined);
      return;
    }
    put(key, { status: 'loading' });
    rpc(host, 'trunkCommits', { repo, newer, older }).then(
      (commits) => put(key, { status: 'ok', commits }),
      (e: unknown) => put(key, { status: 'error', reason: errorText(e, t) }),
    );
  };

  if (view.sections.length === 0) return <div class="muted pad">{t('graph.empty')}</div>;
  return (
    <div class="graph">
      {view.sections.map((section) => {
        const rows = section.rows;
        const spans = spansOf(rows);
        const width = LANE_X + section.lanes * LANE_STEP;
        const colorOf = (name: string) => {
          const b = view.branches[name];
          return b ? branchColor(theme, b) : '';
        };
        const seen = new Set<string>(); // the first row of a branch carries its name and chip

        const nodes = rows.map((r, i) => {
          const h = heightOf(r.kind);
          const geo = rowGeometry(rows, spans, i);
          const x = laneX(r.lane);
          const cy = h / 2;
          const b: BranchView | undefined = r.branch ? view.branches[r.branch] : undefined;
          const color = b ? branchColor(theme, b) : '';
          const first = !seen.has(r.branch);
          if (r.branch) seen.add(r.branch);
          const rowKey = `${section.trunk}:${i}`;
          const top = b ? topChip(chips(b, t)) : null;
          const branchTip = b ? (top ? `${b.name} · ${top.text}` : b.name) : r.branch;
          const trunkTip = t('graph.trunkTip', { hash: r.hash.slice(0, 7) });

          // Is this node the one the details are about?
          const isTrunkPick = pick?.kind === 'trunk' && (r.kind === 'head' || r.kind === 'fork') && pick.hash === r.hash;
          const isCommitPick = pick?.kind === 'commit' && r.kind === 'commit' && pick.branch === r.branch && pick.hash === r.hash;
          const isBranchPick =
            first && !!selected && r.branch === selected && (r.kind === 'commit' || r.kind === 'merge') && pick?.kind !== 'commit';
          const on = isTrunkPick || isCommitPick || isBranchPick;
          const ring = on ? ' g-ring' : '';
          const hl = hover && r.branch && hover === r.branch ? ' g-hl' : '';

          const hit = (tip: string) => (
            <circle class="g-hit" cx={r.kind === 'head' || r.kind === 'fork' ? LANE_X : x} cy={cy} r={HIT_R}>
              <title>{tip}</title>
            </circle>
          );

          let marks: ComponentChildren = null;
          let text: ComponentChildren = null;
          let textClass = 'g-text';
          let tip = branchTip;
          let click: () => void = () => onSelect(r.branch);
          let label = '';
          let expansion: ComponentChildren = null;
          let expanded: boolean | undefined;

          if (r.kind === 'head') {
            tip = trunkTip;
            click = () => onTrunk({ trunk: section.trunk, hash: r.hash, time: r.time, subject: '' });
            marks = (
              <>
                <circle class={`g-dot g-trunk-dot${ring}`} cx={x} cy={cy} r={5} />
                {hit(tip)}
              </>
            );
            text = <span class="g-name">{t('graph.head', { name: section.trunk })}</span>;
          } else if (r.kind === 'fork') {
            tip = trunkTip;
            label = t('graph.fork');
            click = () => onTrunk({ trunk: section.trunk, hash: r.hash, time: r.time, subject: '' });
            marks = (
              <>
                <circle class={`g-dot g-hollow${ring}`} cx={LANE_X} cy={cy} r={4} />
                {hit(tip)}
              </>
            );
          } else if (r.kind === 'merge') {
            textClass += ' dim';
            marks = (
              <>
                <path
                  class={`g-line g-hook${hl}`}
                  style={{ stroke: color || undefined }}
                  d={`M ${x} ${cy} C ${x - 14} ${cy}, ${LANE_X} ${cy}, ${LANE_X} ${h}`}
                  fill="none"
                  stroke-dasharray="3 3"
                  opacity={0.5}
                />
                <circle class={`g-dot g-faint${ring}${hl}`} style={{ fill: color || undefined }} cx={x} cy={cy} r={3} />
                {hit(tip)}
              </>
            );
            text = <span class="g-name">{b?.short ?? r.branch}</span>;
          } else if (r.kind === 'more-own') {
            textClass += ' dim italic';
            text = <span class="g-name">{t('graph.moreOwn', { n: r.count })}</span>;
          } else if (r.kind === 'more') {
            textClass += ' dim italic';
            tip = '';
            const anchors = anchorsOf(rows, i);
            const key = anchors ? `${repo}|${anchors.newer}|${anchors.older}` : '';
            const state = key ? fetched.current[key] : undefined;
            expanded = anchors ? state?.status === 'ok' : undefined;
            click = () => anchors && toggle(key, anchors.newer, anchors.older);
            text = <span class="g-name">{t('graph.moreTrunk', { n: r.count })}</span>;
            if (anchors && state) {
              expansion = (
                <Expanded
                  t={t}
                  state={state}
                  total={r.count}
                  width={width}
                  segs={rowGeometry(rows, spans, i, true, EXPANDED_H).segs}
                  hover={hover}
                  colorOf={colorOf}
                  picked={pick?.kind === 'trunk' ? pick.hash : ''}
                  onPick={(c) => {
                    setAnchor(rowKey);
                    onTrunk({ trunk: section.trunk, hash: c.hash, time: c.time, subject: c.subject });
                  }}
                  onRetry={() => toggle(key, anchors.newer, anchors.older)}
                />
              );
            }
          } else {
            // commit: the first row of a branch is the branch, the others are its commits
            marks = (
              <>
                <circle class={`g-dot${ring}${hl}`} style={{ fill: color || undefined }} cx={x} cy={cy} r={4} />
                {hit(tip)}
              </>
            );
            if (first) {
              text = (
                <>
                  <span class="g-name">{b?.short ?? r.branch}</span>
                  {top && <span class={`chip ${top.tone}`}>{top.text}</span>}
                </>
              );
            } else {
              textClass += ' dim';
              const subject = b?.own.find((c) => c.hash === r.hash)?.subject ?? r.hash.slice(0, 7);
              text = <span class="g-name">{subject}</span>;
              click = () => onCommit(r.branch, r.hash);
            }
          }

          const mine = r.kind === 'head' || r.kind === 'fork' ? '' : r.branch;
          return (
            <>
              <button
                class={`g-row${on ? ' selected' : ''}`}
                data-kind={r.kind}
                style={{ height: `${h}px` }}
                title={tip || undefined}
                aria-label={label || undefined}
                aria-expanded={expanded}
                onClick={() => {
                  if (r.kind !== 'more') setAnchor(rowKey);
                  click();
                }}
                onMouseEnter={() => setHover(mine)}
                onMouseLeave={() => setHover('')}
                onFocus={() => setHover(mine)}
                onBlur={() => setHover('')}
              >
                <Gutter width={width} height={h} segs={geo.segs} curves={geo.curves} hover={hover} colorOf={colorOf}>
                  {marks}
                </Gutter>
                <span class={textClass}>{text}</span>
              </button>
              {expansion}
              {inline && anchor === rowKey && <div class="g-inline">{inline}</div>}
            </>
          );
        });

        return (
          <section class="g-section" key={section.trunk}>
            <div class="g-title">{t('group.trunk', { name: section.trunk })}</div>
            {nodes}
          </section>
        );
      })}
    </div>
  );
}

// anchorsOf finds the trunk commits a "more" row hides between: the head or fork
// row above it and the fork row below it.
function anchorsOf(rows: Row[], i: number): { newer: string; older: string } | null {
  let newer = '';
  for (let j = i - 1; j >= 0 && !newer; j--) if (rows[j].kind === 'head' || rows[j].kind === 'fork') newer = rows[j].hash;
  let older = '';
  for (let j = i + 1; j < rows.length && !older; j++) if (rows[j].kind === 'fork') older = rows[j].hash;
  return newer && older ? { newer, older } : null;
}

interface ExpandedProps {
  t: T;
  state: Fetched;
  total: number;
  width: number;
  segs: Seg[];
  hover: string;
  colorOf(branch: string): string;
  picked: string; // hash of the picked trunk commit
  onPick(c: CommitInfo): void;
  onRetry(): void;
}

function Expanded({ t, state, total, width, segs, hover, colorOf, picked, onPick, onRetry }: ExpandedProps) {
  const note = (text: ComponentChildren) => (
    <div class="g-row g-expanded" style={{ height: `${EXPANDED_H}px` }}>
      <Gutter width={width} height={EXPANDED_H} segs={segs} hover={hover} colorOf={colorOf} />
      <span class="g-text dim">{text}</span>
    </div>
  );
  if (state.status === 'loading') return note(<span class="g-name">{t('graph.loading')}</span>);
  if (state.status === 'error') {
    return note(
      <>
        <span class="g-name">{t('graph.loadFailed', { reason: state.reason })}</span>{' '}
        <button class="btn" onClick={onRetry}>
          {t('app.retry')}
        </button>
      </>,
    );
  }
  return (
    <>
      {state.commits.map((c) => (
        <button
          class={`g-row g-expanded${picked === c.hash ? ' selected' : ''}`}
          data-kind="trunk-commit"
          style={{ height: `${EXPANDED_H}px` }}
          key={c.hash}
          title={t('graph.trunkTip', { hash: c.hash.slice(0, 7) })}
          onClick={() => onPick(c)}
        >
          <Gutter width={width} height={EXPANDED_H} segs={segs} hover={hover} colorOf={colorOf}>
            <circle class={`g-dot g-trunk-dot${picked === c.hash ? ' g-ring' : ''}`} cx={LANE_X} cy={EXPANDED_H / 2} r={2.5} />
            <circle class="g-hit" cx={LANE_X} cy={EXPANDED_H / 2} r={HIT_R} />
          </Gutter>
          <span class="g-text">
            <span class="g-name">{c.subject}</span>
          </span>
        </button>
      ))}
      {state.commits.length < total && note(<span class="g-name">{t('graph.partial', { shown: state.commits.length, total })}</span>)}
    </>
  );
}
