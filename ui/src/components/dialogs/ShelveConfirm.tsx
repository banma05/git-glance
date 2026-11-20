import type { T } from '../../i18n';
import type { Actions, AppState } from '../../state';
import { FileList } from './parts';

interface Props {
  t: T;
  state: Pick<AppState, 'ask' | 'repo' | 'busy' | 'gitProblem'>;
  actions: Pick<Actions, 'confirmShelve'>;
}

// The core names at most this many files when it refuses a switch.
const LISTED_FILES_CAP = 50;

// ShelveConfirm lists the uncommitted changes a switch of the main folder
// would shelve, and says where they are kept and when they come back. Only its
// button sends the switch that shelves.
export function ShelveConfirm({ t, state, actions }: Props) {
  const a = state.ask;
  if (!a) return null;
  const moved = a.repo !== state.repo;
  return (
    <div>
      <p>{t('dialog.shelve.intro', { folder: a.folder, branch: a.target.branch })}</p>
      {a.files.length > 0 ? <FileList files={a.files} /> : <p class="muted">{t('dialog.shelve.unknown')}</p>}
      {a.files.length >= LISTED_FILES_CAP && <p class="muted">{t('dialog.shelve.capped')}</p>}
      <p class="muted">{t('dialog.shelve.how')}</p>
      <p class="muted">{t('dialog.shelve.later')}</p>
      {moved && <p class="warn">{t('dialog.repoChanged')}</p>}
      <button class="btn" disabled={moved || state.busy || !!state.gitProblem} onClick={actions.confirmShelve}>
        {t('dialog.shelve.confirm')}
      </button>
    </div>
  );
}
