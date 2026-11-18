import type { Host } from '../host/types';
import type { T } from '../i18n';
import { chips } from '../format';
import { baseName } from '../reasons';
import { trunkKey } from '../state';
import type { Actions, AppState } from '../state';
import type { BranchView } from '../types';
import { DetailPanel } from './DetailPanel';
import { Details } from './Details';
import { GraphView } from './GraphView';
import { branchColor } from './ListView';

interface Props {
  t: T;
  host: Host;
  state: AppState;
  actions: Actions;
}

function Fish({ t, b, state, actions }: Omit<Props, 'host'> & { b: BranchView }) {
  const first = chips(b, t)[0];
  const sub = [b.folder ? baseName(b.folder) : '', first?.text ?? ''].filter(Boolean).join(' · ');
  const open = state.selected === b.name;
  return (
    <div class="fish">
      <button
        class={`row${open ? ' selected' : ''}${b.merged ? ' merged' : ''}`}
        title={b.name}
        onClick={() => actions.select(b.name)}
      >
        <span class="dot" style={{ background: branchColor(state.theme, b) }} />
        <span class="row-main">
          <span class="row-name">{b.topic}</span>
          {sub && <span class="row-sub muted">{sub}</span>}
        </span>
      </button>
      {open && <DetailPanel t={t} branch={b} state={state} actions={actions} />}
    </div>
  );
}

// Sidebar is the narrow mode: a repository dropdown, the list/graph toggle and
// the button that opens the wide window, then a vertical list of branches.
export function Sidebar({ t, host, state, actions }: Props) {
  const view = state.view;
  const list = (names: string[]) =>
    names
      .map((n) => view?.branches[n])
      .filter((b): b is BranchView => !!b)
      .map((b) => <Fish key={b.name} t={t} b={b} state={state} actions={actions} />);
  const group = (key: string, title: string, names: string[]) => (
    <section class="group">
      <button class="group-head" aria-expanded={!state.collapsed[key]} onClick={() => actions.toggleGroup(key)}>
        <span aria-hidden="true">{state.collapsed[key] ? '▸' : '▾'} </span>
        {title}
        <span class="muted"> ({names.length})</span>
      </button>
      {!state.collapsed[key] && list(names)}
    </section>
  );

  return (
    <div class="sidebar">
      <div class="side-head">
        <select
          aria-label={t('repo.select')}
          value={state.repo}
          onChange={(e) => actions.selectRepo((e.target as HTMLSelectElement).value)}
        >
          {state.repos.map((r) => (
            <option value={r}>{baseName(r)}</option>
          ))}
        </select>
        <div class="seg" role="group">
          <button class="btn" aria-pressed={state.mode === 'list'} onClick={() => actions.setMode('list')}>
            {t('toolbar.list')}
          </button>
          <button class="btn" aria-pressed={state.mode === 'graph'} onClick={() => actions.setMode('graph')}>
            {t('toolbar.graph')}
          </button>
        </div>
        <button class="btn" title={t('toolbar.wide')} aria-label={t('toolbar.wide')} onClick={actions.openWide}>
          ⤢
        </button>
      </div>
      {view && state.mode === 'graph' && (
        <>
          <GraphView
            t={t}
            host={host}
            repo={state.repo}
            view={view}
            theme={state.theme}
            selected={state.selected}
            pick={state.pick}
            onSelect={actions.select}
            onCommit={actions.selectCommit}
            onTrunk={actions.selectTrunk}
            inline={state.selected || state.pick ? <Details t={t} view={view} state={state} actions={actions} /> : undefined}
          />
        </>
      )}
      {view && state.mode === 'list' && (
        <div class="list compact">
          {view.groups.map((g) => group(trunkKey(g.trunk.name), t('group.trunk', { name: g.trunk.name }), g.branches))}
          {view.unknown.length > 0 && group('unknown', t('group.unknown'), view.unknown)}
          {view.old.length > 0 && group('old', t('group.old'), view.old)}
        </div>
      )}
    </div>
  );
}
