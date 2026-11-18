import { useEffect, useState } from 'preact/hooks';
import { rpc } from '../../api';
import { formatBytes } from '../../format';
import type { Host } from '../../host/types';
import type { T } from '../../i18n';
import { errorText, reasonText } from '../../reasons';
import type { Actions, AppState } from '../../state';
import type { BackupsInfo } from '../../types';
import { FileList, ResultNotes } from './parts';

interface Props {
  t: T;
  host: Host;
  state: AppState;
  actions: Pick<Actions, 'removeFolder'>;
}

// RemoveFolderConfirm states what removing a folder does and calls removeFolder
// only when the confirm button is clicked.
export function RemoveFolderConfirm({ t, host, state, actions }: Props) {
  const branch = state.view && state.selected ? (state.view.branches[state.selected] ?? null) : null;
  const [backups, setBackups] = useState<BackupsInfo | null>(null);
  useEffect(() => {
    rpc(host, 'backups')
      .then(setBackups)
      .catch(() => setBackups(null));
  }, [host]);

  const name = state.selected;
  const out = state.removeOutcome && state.removeOutcome.branch === name ? state.removeOutcome : null;
  if (!branch && !out) return <p class="muted">{t('dialog.remove.noBranch')}</p>;
  const r = out?.result ?? null;
  const done = !!r?.ok;
  return (
    <div>
      {branch && !done && (
        <>
          <p>{t('dialog.remove.intro', { branch: branch.name, path: branch.folder })}</p>
          <ul class="rules">
            <li>{t('dialog.remove.rule.disk')}</li>
            <li>{t('dialog.remove.rule.branch')}</li>
            <li>{t('dialog.remove.rule.backup', { dir: backups?.dir ?? '...' })}</li>
            <li>{t('dialog.remove.rule.build')}</li>
            <li>{t('dialog.remove.rule.changed')}</li>
          </ul>
          <div class="dialog-actions">
            <button class="btn danger" disabled={state.busy} onClick={() => actions.removeFolder(name)}>
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
