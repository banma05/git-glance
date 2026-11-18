import type { T } from '../i18n';
import type { Actions, ViewMode } from '../state';
import type { Theme } from '../types';

export const EDITORS = ['code', 'cursor', 'windsurf', 'idea', 'pycharm', 'webstorm', 'goland'];

interface Props {
  t: T;
  actions: Actions;
  mode: ViewMode;
  busy: boolean;
  readOnly: boolean; // the core refuses every change (git too old or missing)
  editor: string;
  theme: Theme;
  showEditor: boolean; // only the browser opens folders with a command chosen here
}

const THEME_ICON: Record<Theme, string> = { auto: '◐', dark: '☾', light: '☀' };

// Toolbar is the wide window's row of operations. There is no button that
// changes the remote; the only network action is reading it.
export function Toolbar({ t, actions, mode, busy, readOnly, editor, theme, showEditor }: Props) {
  return (
    <div class="toolbar">
      <button class="btn" disabled={busy || readOnly} onClick={actions.align}>
        <span aria-hidden="true">⟳ </span>
        {t('toolbar.align')}
      </button>
      <button class="btn" disabled={busy || readOnly} onClick={() => actions.openDialog('create')}>
        <span aria-hidden="true">＋ </span>
        {t('toolbar.createFolder')}
      </button>
      <button class="btn" onClick={() => actions.openDialog('compare')}>
        <span aria-hidden="true">⇄ </span>
        {t('toolbar.compare')}
      </button>
      <button class="btn" onClick={() => actions.openDialog('oplog')}>
        <span aria-hidden="true">≡ </span>
        {t('toolbar.oplog')}
      </button>
      {busy && <span class="muted">{t('toolbar.working')}</span>}
      <span class="spacer" />
      <div class="seg" role="group">
        <button class="btn" aria-pressed={mode === 'list'} onClick={() => actions.setMode('list')}>
          {t('toolbar.list')}
        </button>
        <button class="btn" aria-pressed={mode === 'graph'} onClick={() => actions.setMode('graph')}>
          {t('toolbar.graph')}
        </button>
      </div>
      {showEditor && (
        <label class="field">
          {t('toolbar.openWith')}
          <select disabled={readOnly} value={editor} onChange={(e) => actions.setEditor((e.target as HTMLSelectElement).value)}>
            {EDITORS.map((id) => (
              <option value={id}>{id}</option>
            ))}
          </select>
        </label>
      )}
      <button class="btn" disabled={readOnly} onClick={actions.cycleTheme}>
        <span aria-hidden="true">{THEME_ICON[theme]} </span>
        {t(`toolbar.theme.${theme}`)}
      </button>
      <button class="btn" onClick={() => actions.openDialog('settings')}>
        <span aria-hidden="true">⚙ </span>
        {t('toolbar.settings')}
      </button>
    </div>
  );
}
