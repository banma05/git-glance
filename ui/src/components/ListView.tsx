import { useRef } from 'preact/hooks';
import type { T } from '../i18n';
import { chips } from '../format';
import { PALETTE } from '../palette';
import { BranchName } from './BranchName';
import { baseName } from '../reasons';
import { trunkKey } from '../state';
import type { Actions, AppState } from '../state';
import type { BranchView, View } from '../types';

interface Props {
  t: T;
  view: View;
  state: Pick<AppState, 'selected' | 'selectedTrunk' | 'collapsed' | 'theme'>;
  actions: Pick<Actions, 'select' | 'toggleGroup' | 'moveTrunk' | 'showTrunk'>;
  compact?: boolean; // the left column next to the graph: names only
}

export function branchColor(theme: 'dark' | 'light', b: BranchView): string {
  const colors = PALETTE[theme];
  return colors[((b.color % colors.length) + colors.length) % colors.length];
}

interface RowProps {
  t: T;
  b: BranchView;
  theme: 'dark' | 'light';
  selected: boolean;
  compact: boolean;
  onSelect(): void;
}

function BranchRow({ t, b, theme, selected, compact, onSelect }: RowProps) {
  const list = chips(b, t);
  return (
    <button class={`row${selected ? ' selected' : ''}${b.merged ? ' merged' : ''}`} onClick={onSelect} title={b.name}>
      <span class="dot" style={{ background: branchColor(theme, b) }} />
      <span class="row-main">
        <BranchName class="row-name" name={b.short} full={b.name} />
        {!compact && (
          <span class="row-sub">
            {b.folder && <span class="muted">{baseName(b.folder)}</span>}
            {list.map((c) => (
              <span class={`chip ${c.tone}`}>{c.text}</span>
            ))}
          </span>
        )}
      </span>
    </button>
  );
}

interface HeadProps {
  t: T;
  title: string;
  count: number;
  folded: boolean;
  onFold(): void;
  // a trunk's group: its name shows the trunk's details, the arrow folds
  trunk?: { name: string; selected: boolean; hint: string; onShow(): void };
  drag?: Record<string, unknown>; // what makes the header draggable
}

// GroupHead is the header of a group. The header of a trunk has two buttons:
// the arrow only folds the group, the name shows (or hides) the trunk's details.
export function GroupHead({ t, title, count, folded, onFold, trunk, drag }: HeadProps) {
  const arrow = <span aria-hidden="true">{folded ? '▸' : '▾'} </span>;
  if (!trunk) {
    return (
      <button class="group-head" aria-expanded={!folded} onClick={onFold}>
        {arrow}
        {title}
        <span class="muted"> ({count})</span>
      </button>
    );
  }
  const foldLabel = t('group.fold', { name: trunk.name });
  return (
    <div class={`group-head trunk-head${trunk.selected ? ' selected' : ''}`} {...drag}>
      <button class="group-fold" aria-expanded={!folded} aria-label={foldLabel} title={foldLabel} onClick={onFold}>
        {arrow}
      </button>
      <button class="group-name" aria-pressed={trunk.selected} title={trunk.hint} onClick={trunk.onShow}>
        {title}
        <span class="muted"> ({count})</span>
      </button>
    </div>
  );
}

// ListView is the branches grouped by trunk, then "cannot tell" and "old
// branches". Trunk groups fold, show their trunk's details and can be dragged
// to a new place.
export function ListView({ t, view, state, actions, compact = false }: Props) {
  const dragFrom = useRef('');

  const rows = (names: string[]) =>
    names
      .map((n) => view.branches[n])
      .filter(Boolean)
      .map((b) => (
        <BranchRow
          key={b.name}
          t={t}
          b={b}
          theme={state.theme}
          selected={state.selected === b.name}
          compact={compact}
          onSelect={() => actions.select(b.name)}
        />
      ));

  const header = (key: string, title: string, count: number) => (
    <GroupHead t={t} title={title} count={count} folded={!!state.collapsed[key]} onFold={() => actions.toggleGroup(key)} />
  );

  return (
    <div class={`list${compact ? ' compact' : ''}`}>
      {view.groups.map((g, index) => {
        const key = trunkKey(g.trunk.name);
        const list = rows(g.branches);
        return (
          <section
            class="group"
            onDragOver={(e) => e.preventDefault()}
            onDrop={(e) => {
              e.preventDefault();
              const from = dragFrom.current;
              dragFrom.current = '';
              if (from && from !== g.trunk.name) actions.moveTrunk(from, index);
            }}
          >
            <GroupHead
              t={t}
              title={t('group.trunk', { name: g.trunk.name })}
              count={g.branches.length}
              folded={!!state.collapsed[key]}
              onFold={() => actions.toggleGroup(key)}
              trunk={{
                name: g.trunk.name,
                selected: state.selectedTrunk === g.trunk.name,
                hint: t('group.dragHint'),
                onShow: () => actions.showTrunk(g.trunk.name),
              }}
              drag={{
                draggable: true,
                onDragStart: () => {
                  dragFrom.current = g.trunk.name;
                },
              }}
            />
            {!state.collapsed[key] && (list.length > 0 ? list : <div class="muted pad">{t('group.empty')}</div>)}
          </section>
        );
      })}
      {view.unknown.length > 0 && (
        <section class="group">
          {header('unknown', t('group.unknown'), view.unknown.length)}
          {!state.collapsed.unknown && rows(view.unknown)}
        </section>
      )}
      {view.old.length > 0 && (
        <section class="group">
          {header('old', t('group.old'), view.old.length)}
          {!state.collapsed.old && rows(view.old)}
        </section>
      )}
    </div>
  );
}
