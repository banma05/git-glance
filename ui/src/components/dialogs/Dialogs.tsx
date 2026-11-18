import type { T } from '../../i18n';
import { reasonText } from '../../reasons';
import type { Actions, AppState, DialogName } from '../../state';

const TITLES: Record<DialogName, 'toolbar.align' | 'toolbar.createFolder' | 'toolbar.compare' | 'toolbar.oplog' | 'toolbar.settings'> = {
  align: 'toolbar.align',
  create: 'toolbar.createFolder',
  compare: 'toolbar.compare',
  oplog: 'toolbar.oplog',
  settings: 'toolbar.settings',
  remove: 'toolbar.createFolder',
};

// PLACEHOLDER (task 15c replaces this file with the real dialogs). It shows the
// dialog's name and, for the align report, a short summary so the data is
// visible while the real dialog is being built.
export function Dialogs({ t, state, actions }: { t: T; state: AppState; actions: Actions }) {
  const name = state.dialog;
  if (!name) return null;
  const report = name === 'align' ? state.alignReport : null;
  return (
    <div class="overlay" role="dialog" aria-modal="true">
      <div class="dialog">
        <h3>{t(TITLES[name])}</h3>
        {report ? (
          <div>
            <p>{t('dialog.align.summary', { updated: report.updated.length, skipped: report.skipped.length })}</p>
            <ul>
              {report.skipped.map((s) => (
                <li>
                  {s.branch}: {reasonText('align.skip', s.reason, t)}
                </li>
              ))}
            </ul>
          </div>
        ) : (
          <p>{t('dialog.later')}</p>
        )}
        <button class="btn" onClick={actions.closeDialog}>
          {t('app.close')}
        </button>
      </div>
    </div>
  );
}
