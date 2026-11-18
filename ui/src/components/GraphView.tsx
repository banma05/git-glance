import type { ComponentChildren } from 'preact';
import { useRef, useState } from 'preact/hooks';
import { rpc } from '../api';
import { chips, shortDate } from '../format';
import type { Host } from '../host/types';
import type { T } from '../i18n';
import { baseName, errorText } from '../reasons';
import type { BranchView, CommitInfo, Row, Section, View } from '../types';
import { EXPANDED_H, LANE_STEP, LANE_X, heightOf, laneX, rowGeometry, spansOf } from './graphGeometry';
import type { Curve, Seg } from './graphGeometry';
import { branchColor } from './ListView';

interface GutterProps {
  width: number;
  height: number;
  segs: Seg[];
  curves?: Curve[];
  colorOf(branch: string): string;
  children?: ComponentChildren;
}

function Gutter({ width, height, segs, curves = [], colorOf, children }: GutterProps) {
  return (
    <svg class="g-gutter" width={width} height={height} viewBox={`0 0 ${width} ${height}`} aria-hidden="true">
      {segs.map((l) => {
        const color = l.branch ? colorOf(l.branch) : '';
        return (
          <line
            class={l.branch ? 'g-line' : 'g-line g-trunk'}
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
        <path class="g-line" style={{ stroke: colorOf(c.branch) || undefined }} d={c.d} fill="none" />
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
    const geo = rowGeometry(rows, spans, i);
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
            segs={rowGeometry(rows, spans, i, true, EXPANDED_H).segs}
            colorOf={colorOf}
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
          <Gutter width={width} height={h} segs={geo.segs} curves={geo.curves} colorOf={colorOf}>
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
  segs: Seg[];
  colorOf(branch: string): string;
  onRetry(): void;
}

function Expanded({ t, state, total, width, segs, colorOf, onRetry }: ExpandedProps) {
  const note = (text: ComponentChildren) => (
    <div class="g-row g-expanded" style={{ height: `${EXPANDED_H}px` }}>
      <Gutter width={width} height={EXPANDED_H} segs={segs} colorOf={colorOf} />
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
          <Gutter width={width} height={EXPANDED_H} segs={segs} colorOf={colorOf}>
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
