import type { ComponentChildren } from 'preact';
import { useRef, useState } from 'preact/hooks';
import { rpc } from '../api';
import { chips, shortDate } from '../format';
import type { Host } from '../host/types';
import type { T } from '../i18n';
import { baseName, errorText } from '../reasons';
import type { BranchView, CommitInfo, Row, Section, View } from '../types';
import { branchColor } from './ListView';

const LANE_X = 22; // the trunk is lane 0; lane n sits at 22 + n * 22
const LANE_STEP = 22;
const ROW_H = 34;
const SHORT_H = 26; // more, more-own and fork rows
const EXPANDED_H = 22; // one hidden trunk commit

const laneX = (lane: number) => LANE_X + lane * LANE_STEP;
const heightOf = (kind: Row['kind']) => (kind === 'more' || kind === 'more-own' || kind === 'fork' ? SHORT_H : ROW_H);

// A Span is the stretch of one lane that belongs to one branch: from its first
// row to its last row (the row where it joins the trunk, or its last commit).
interface Span {
  lane: number;
  branch: string;
  start: number;
  end: number;
  forkFirst: boolean; // opened by a fork row, so that row is not the end
}

function spansOf(rows: Row[]): Span[] {
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

interface Line {
  lane: number;
  color: string; // '' for the trunk
  top: boolean;
  bottom: boolean;
  dashed: boolean;
}

interface GutterProps {
  width: number;
  height: number;
  lines: Line[];
  children?: ComponentChildren;
}

function Gutter({ width, height, lines, children }: GutterProps) {
  return (
    <svg class="g-gutter" width={width} height={height} viewBox={`0 0 ${width} ${height}`} aria-hidden="true">
      {lines.map((l) => {
        const x = laneX(l.lane);
        const style = l.color ? { stroke: l.color } : undefined;
        const cls = l.color ? 'g-line' : 'g-line g-trunk';
        const dash = l.dashed ? '3 3' : undefined;
        return (
          <>
            {l.top && <line class={cls} style={style} x1={x} x2={x} y1={0} y2={height / 2} stroke-dasharray={dash} />}
            {l.bottom && <line class={cls} style={style} x1={x} x2={x} y1={height / 2} y2={height} stroke-dasharray={dash} />}
          </>
        );
      })}
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
  onSelect(branch: string): void;
}

// GraphView draws one block per trunk. Each row is a small SVG (the lanes) next
// to its text, so the text wraps and scrolls like any other.
export function GraphView({ t, host, repo, view, theme, selected, onSelect }: Props) {
  const fetched = useRef<Record<string, Fetched>>({});
  const [, redraw] = useState(0);
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
      {view.sections.map((s) => (
        <SectionBlock
          key={s.trunk}
          t={t}
          view={view}
          section={s}
          repo={repo}
          theme={theme}
          selected={selected}
          onSelect={onSelect}
          fetched={fetched.current}
          toggle={toggle}
        />
      ))}
    </div>
  );
}

interface BlockProps {
  t: T;
  view: View;
  section: Section;
  repo: string;
  theme: 'dark' | 'light';
  selected: string;
  onSelect(branch: string): void;
  fetched: Record<string, Fetched>;
  toggle(key: string, newer: string, older: string): void;
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

function SectionBlock({ t, view, section, repo, theme, selected, onSelect, fetched, toggle }: BlockProps) {
  const rows = section.rows;
  const spans = spansOf(rows);
  const width = LANE_X + section.lanes * LANE_STEP;
  const colorOf = (name: string) => {
    const b = view.branches[name];
    return b ? branchColor(theme, b) : '';
  };
  const seen = new Set<string>(); // a branch shows its name and chips on its first row only

  // linesAt lists the vertical lines in row i. With below=true it is the stretch
  // just under the row (inside an expanded list), where every open line keeps running.
  const linesAt = (i: number, kind: Row['kind'], branch: string, below: boolean): Line[] => {
    const out: Line[] = [];
    if (rows.length > 1) {
      out.push({ lane: 0, color: '', top: below || i > 0, bottom: below || i < rows.length - 1, dashed: !below && kind === 'more' });
    }
    for (const s of spans) {
      if (i < s.start || i > s.end || (below && i >= s.end)) continue;
      out.push({
        lane: s.lane,
        color: colorOf(s.branch),
        top: below || i > s.start,
        bottom: below || i < s.end,
        dashed: !below && kind === 'more-own' && s.branch === branch,
      });
    }
    return out;
  };

  const branchLine = (r: Row, b: BranchView | undefined, extra: ComponentChildren) => (
    <button class="g-main" title={r.branch} onClick={() => onSelect(r.branch)}>
      <span class="g-name" title={r.branch}>
        {b?.short ?? r.branch}
      </span>
      <span class="g-sub">
        {extra}
        {b?.folder && <span class="muted">{baseName(b.folder)}</span>}
        {b && chips(b, t).map((c) => <span class={`chip ${c.tone}`}>{c.text}</span>)}
      </span>
    </button>
  );

  const nodes = rows.map((r, i) => {
    const h = heightOf(r.kind);
    const x = laneX(r.lane);
    const cy = h / 2;
    const b: BranchView | undefined = r.branch ? view.branches[r.branch] : undefined;
    const color = b ? branchColor(theme, b) : '';
    const first = !seen.has(r.branch);
    if (r.branch) seen.add(r.branch);

    let marks: ComponentChildren = null;
    let text: ComponentChildren = null;
    let textClass = 'g-text';
    let expansion: ComponentChildren = null;

    if (r.kind === 'head') {
      marks = <circle class="g-dot g-trunk-dot" cx={x} cy={cy} r={5} />;
      text = <span class="g-name">{t('graph.head', { name: section.trunk })}</span>;
    } else if (r.kind === 'fork') {
      marks = (
        <>
          {r.ends.map((lane) => {
            const owner = spans.find((s) => s.lane === lane && s.start <= i && i <= s.end);
            return (
              <path
                class="g-line"
                style={{ stroke: colorOf(owner?.branch ?? '') || undefined }}
                d={`M ${laneX(lane)} 0 C ${laneX(lane)} ${cy}, ${LANE_X + 8} ${cy}, ${LANE_X} ${cy}`}
                fill="none"
              />
            );
          })}
          <circle class="g-dot g-hollow" cx={LANE_X} cy={cy} r={4} />
        </>
      );
      textClass += ' dim';
      text = (
        <>
          <span class="g-name">{t('graph.fork')}</span>
          {r.hash && <span class="g-sub muted">{r.hash.slice(0, 7)}</span>}
        </>
      );
    } else if (r.kind === 'merge') {
      marks = (
        <>
          <path
            class="g-line g-hook"
            style={{ stroke: color || undefined }}
            d={`M ${x} ${cy} C ${x - 14} ${cy}, ${LANE_X} ${cy}, ${LANE_X} ${h}`}
            fill="none"
            stroke-dasharray="3 3"
            opacity={0.5}
          />
          <circle class="g-dot g-faint" style={{ fill: color || undefined }} cx={x} cy={cy} r={3} />
        </>
      );
      textClass += ' dim';
      text = branchLine(r, b, <span class="muted">{r.count > 0 ? t('graph.mergedCount', { n: r.count }) : t('graph.merged')}</span>);
    } else if (r.kind === 'more-own') {
      textClass += ' dim italic';
      text = <span class="g-name">{t('graph.moreOwn', { n: r.count })}</span>;
    } else if (r.kind === 'more') {
      textClass += ' dim italic';
      const anchors = anchorsOf(rows, i);
      const key = anchors ? `${repo}|${anchors.newer}|${anchors.older}` : '';
      const state = key ? fetched[key] : undefined;
      const label = <span class="g-name">{t('graph.moreTrunk', { n: r.count })}</span>;
      text = anchors ? (
        <button class="g-main g-more" aria-expanded={state?.status === 'ok'} onClick={() => toggle(key, anchors.newer, anchors.older)}>
          {label}
        </button>
      ) : (
        label
      );
      if (anchors && state) {
        expansion = (
          <Expanded
            t={t}
            state={state}
            total={r.count}
            width={width}
            lines={linesAt(i, r.kind, r.branch, true)}
            onRetry={() => toggle(key, anchors.newer, anchors.older)}
          />
        );
      }
    } else if (first) {
      marks = <circle class="g-dot" style={{ fill: color || undefined }} cx={x} cy={cy} r={4} />;
      text = branchLine(r, b, null);
    } else {
      marks = <circle class="g-dot" style={{ fill: color || undefined }} cx={x} cy={cy} r={4} />;
      const subject = b?.own.find((c) => c.hash === r.hash)?.subject ?? r.hash.slice(0, 7);
      text = (
        <button class="g-main" title={r.branch} onClick={() => onSelect(r.branch)}>
          <span class="g-name dim">{subject}</span>
        </button>
      );
    }

    return (
      <>
        <div
          class={`g-row${r.branch && r.branch === selected ? ' selected' : ''}`}
          data-kind={r.kind}
          style={{ height: `${h}px` }}
        >
          <Gutter width={width} height={h} lines={linesAt(i, r.kind, r.branch, false)}>
            {marks}
          </Gutter>
          <div class={textClass}>{text}</div>
          <span class="g-date muted">{shortDate(r.time)}</span>
        </div>
        {expansion}
      </>
    );
  });

  return (
    <section class="g-section">
      <div class="g-title">{t('group.trunk', { name: section.trunk })}</div>
      {nodes}
    </section>
  );
}

interface ExpandedProps {
  t: T;
  state: Fetched;
  total: number;
  width: number;
  lines: Line[];
  onRetry(): void;
}

function Expanded({ t, state, total, width, lines, onRetry }: ExpandedProps) {
  const note = (text: ComponentChildren) => (
    <div class="g-row g-expanded" style={{ height: `${EXPANDED_H}px` }}>
      <Gutter width={width} height={EXPANDED_H} lines={lines} />
      <div class="g-text dim">{text}</div>
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
        <div class="g-row g-expanded" style={{ height: `${EXPANDED_H}px` }} key={c.hash}>
          <Gutter width={width} height={EXPANDED_H} lines={lines}>
            <circle class="g-dot g-trunk-dot" cx={LANE_X} cy={EXPANDED_H / 2} r={2.5} />
          </Gutter>
          <div class="g-text">
            <span class="g-name" title={c.hash}>
              {c.subject}
            </span>
          </div>
          <span class="g-date muted">{shortDate(c.time)}</span>
        </div>
      ))}
      {state.commits.length < total && note(<span class="g-name">{t('graph.partial', { shown: state.commits.length, total })}</span>)}
    </>
  );
}
