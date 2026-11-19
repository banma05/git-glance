import { useEffect } from 'preact/hooks';
import type { Host } from '../../host/types';
import type { Key, T } from '../../i18n';
import type { Actions, AppState, DialogName } from '../../state';
import { AlignReport } from './AlignReport';
import { Compare } from './Compare';
import { CreateFolder } from './CreateFolder';
import { OpLog } from './OpLog';
import { RemoveFolderConfirm } from './RemoveFolderConfirm';
import { Settings } from './Settings';
import { ShelveConfirm } from './ShelveConfirm';

const TITLES: Record<DialogName, Key> = {
  align: 'toolbar.align',
  create: 'toolbar.createFolder',
  createExisting: 'dialog.createExisting.title',
  compare: 'toolbar.compare',
  oplog: 'toolbar.oplog',
  settings: 'toolbar.settings',
  remove: 'dialog.remove.title',
  shelve: 'dialog.shelve.title',
};

interface Props {
  t: T;
  host: Host;
  state: AppState;
  actions: Actions;
}

// Dialogs shows the open dialog over the page. Esc and the close button close
// it; closing never cancels a write that is running (its result stays in the
// state and the view is read again when it ends).
export function Dialogs({ t, host, state, actions }: Props) {
  const name = state.dialog;
  const { closeDialog } = actions;
  useEffect(() => {
    if (!name) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') closeDialog();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [name, closeDialog]);
  if (!name) return null;

  let body = null;
  if (name === 'align') body = state.alignReport ? <AlignReport t={t} report={state.alignReport} /> : null;
  else if (name === 'create') body = <CreateFolder t={t} host={host} state={state} actions={actions} />;
  else if (name === 'createExisting') body = <CreateFolder t={t} host={host} state={state} actions={actions} existing />;
  else if (name === 'remove') body = <RemoveFolderConfirm t={t} host={host} state={state} actions={actions} />;
  else if (name === 'compare') body = <Compare t={t} host={host} state={state} />;
  else if (name === 'settings') body = <Settings t={t} host={host} state={state} actions={actions} />;
  else if (name === 'shelve') body = <ShelveConfirm t={t} state={state} actions={actions} />;
  else body = <OpLog t={t} host={host} repo={state.repo} />;

  return (
    <div class="overlay" role="presentation">
      <div class="dialog" role="dialog" aria-modal="true" aria-label={t(TITLES[name])}>
        <h3>{t(TITLES[name])}</h3>
        {body}
        <div class="dialog-actions">
          <button class="btn" onClick={closeDialog}>
            {t('app.close')}
          </button>
        </div>
      </div>
    </div>
  );
}
