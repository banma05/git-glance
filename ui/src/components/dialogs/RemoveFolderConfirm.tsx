import { useEffect, useState } from 'preact/hooks';
import { rpc } from '../../api';
import { blockText, formatBytes } from '../../format';
import type { Host } from '../../host/types';
import type { T } from '../../i18n';
import { errorText, reasonText } from '../../reasons';
import type { Actions, AppState } from '../../state';
import type { BackupsInfo, BranchView } from '../../types';
import { FileList, ResultNotes } from './parts';

interface Props {
  t: T;
  host: Host;
  state: AppState;
  actions: Pick<Actions, 'removeFolder'>;
}

// What the dialog showed when it opened. The folder and its HEAD go with the
// removal, and the core refuses (changed) when either is no longer true.
interface Shown {
  name: string;
  folder: string;
  head: string;
}

// whyNot is why the confirm is disabled, "" when it is not: the view read
// since the dialog opened must still allow the removal of the same folder at
// the same commit.
function whyNot(shown: Shown, now: BranchView | null, t: T): string {
  if (!now) return t('dialog.remove.noBranch');
  if (!now.canRemoveFolder) return t('dialog.remove.blocked', { reason: blockText(now.removeBlock, t) });
  if (!shown.head || now.folder !== shown.folder || now.folderHead !== shown.head) return t('dialog.remove.reopen');
  return '';
}

// RemoveFolderConfirm states what removing a folder does and calls removeFolder
// only when the confirm button is clicked.
export function RemoveFolderConfirm({ t, host, state, actions }: Props) {
  const [shown] = useState<Shown | null>(() => {
    const b = state.view && state.selected ? state.view.branches[state.selected] : undefined;
    return b ? { name: b.name, folder: b.folder, head: b.folderHead } : null;
  });
  const [backups, setBackups] = useState<BackupsInfo | null>(null);
  useEffect(() => {
    rpc(host, 'backups')
      .then(setBackups)
      .catch(() => setBackups(null));
  }, [host]);

  const out = shown && state.removeOutcome && state.removeOutcome.branch === shown.name ? state.removeOutcome : null;
  if (!shown) return <p class="muted">{t('dialog.remove.noBranch')}</p>;
  const now = state.view?.branches[shown.name] ?? null;
  const r = out?.result ?? null;
  const done = !!r?.ok;
  const blocked = whyNot(shown, now, t);
  return (
    <div>
      {!done && (
        <>
          <p>{t('dialog.remove.intro', { branch: shown.name, path: shown.folder })}</p>
          <ul class="rules">
            <li>{t('dialog.remove.rule.disk')}</li>
            <li>{t('dialog.remove.rule.branch')}</li>
            <li>{t('dialog.remove.rule.backup', { dir: backups?.dir ?? '...' })}</li>
            <li>{t('dialog.remove.rule.build')}</li>
            <li>{t('dialog.remove.rule.changed')}</li>
          </ul>
          {blocked && !out && (
            <p class="warn" role="status">
              {blocked}
            </p>
          )}
          <div class="dialog-actions">
            <button
              class="btn danger"
              disabled={state.busy || !!state.gitProblem || !!blocked}
              onClick={() => {
                if (!blocked) actions.removeFolder(shown.name, shown.folder, shown.head);
              }}
            >
              {t('dialog.remove.confirm')}
            </button>
          </div>
        </>
      )}
      {out && (
        <div class="outcome" role="status">
          {out.error ? (
            <p class="warn">{errorText(out.error, t)}</p>
          ) : r ? (
            <>
              <p class={r.ok ? 'ok' : 'warn'}>
                {r.ok ? t('dialog.remove.done') : t('dialog.remove.refused', { reason: reasonText('remove.reason', r.reason, t) })}
              </p>
              {r.files.length > 0 && <FileList files={r.files} />}
              {r.backupDir ? (
                <p>{t('dialog.remove.backup', { dir: r.backupDir, size: formatBytes(r.backupBytes) })}</p>
              ) : (
                r.ok && <p class="muted">{t('dialog.remove.noBackup')}</p>
              )}
              {r.deletedBuild.length > 0 && <p>{t('dialog.remove.build', { list: r.deletedBuild.join(', ') })}</p>}
              {r.skippedLinks.length > 0 && <p>{t('dialog.remove.links', { list: r.skippedLinks.join(', ') })}</p>}
              <ResultNotes t={t} detail={r.detail} logError={r.logError} />
            </>
          ) : null}
        </div>
      )}
    </div>
  );
}
