import { useRef } from 'preact/hooks';
import type { T } from '../i18n';
import { chips } from '../format';
import { PALETTE } from '../palette';
import { baseName } from '../reasons';
import { trunkKey } from '../state';
import type { Actions, AppState } from '../state';
import type { BranchView, View } from '../types';

interface Props {
  t: T;
  view: View;
  state: Pick<AppState, 'selected' | 'collapsed' | 'theme'>;
  actions: Pick<Actions, 'select' | 'toggleGroup' | 'moveTrunk'>;
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
        <span class="row-name">{b.short}</span>
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

// ListView is the branches grouped by trunk, then "cannot tell" and "old
// branches". Trunk groups fold and can be dragged to a new place.
export function ListView({ t, view, state, actions, compact = false }: Props) {
  const dragFrom = useRef('');

  const rows = (names: string[]) =>
    names
      .map((n) => view.branches[n])
      .filter(Boolean)
      .map((b) => (
        <BranchRow
          t={t}
          b={b}
          theme={state.theme}
          selected={state.selected === b.name}
          compact={compact}
          onSelect={() => actions.select(b.name)}
        />
      ));

  const header = (key: string, title: string, count: number, extra?: Record<string, unknown>) => (
    <button class="group-head" aria-expanded={!state.collapsed[key]} onClick={() => actions.toggleGroup(key)} {...extra}>
      <span aria-hidden="true">{state.collapsed[key] ? '▸' : '▾'} </span>
      {title}
      <span class="muted"> ({count})</span>
    </button>
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
            {header(key, t('group.trunk', { name: g.trunk.name }), g.branches.length, {
              draggable: true,
              title: t('group.dragHint'),
              onDragStart: () => {
                dragFrom.current = g.trunk.name;
              },
            })}
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
