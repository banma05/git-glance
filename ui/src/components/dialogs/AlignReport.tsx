import type { T } from '../../i18n';
import { reasonText } from '../../reasons';
import type { AlignReport as Report } from '../../types';
import { FileList, ResultNotes } from './parts';

export function AlignReport({ t, report }: { t: T; report: Report }) {
  return (
    <div>
      {!report.fetch.ok && (
        <p class="warn" role="status">
          {t('fetch.failed', { reason: report.fetch.error || t('error.unknown') })}
        </p>
      )}
      <h4>{t('dialog.align.updated')}</h4>
      {report.updated.length === 0 ? (
        <p class="muted">{t('dialog.align.nothingUpdated')}</p>
      ) : (
        <ul class="files">
          {report.updated.map((u) => (
            <li>
              <div>{t('dialog.align.item', { branch: u.branch, from: u.from.slice(0, 7), to: u.to.slice(0, 7) })}</div>
              {u.folder && <div class="muted">{t('dialog.folderLabel', { path: u.folder })}</div>}
            </li>
          ))}
        </ul>
      )}
      {report.skipped.length > 0 && <h4>{t('dialog.align.skipped')}</h4>}
      <ul class="files">
        {report.skipped.map((s) => (
          <li>
            <div>
              <strong>{s.branch}</strong>: {reasonText('align.skip', s.reason, t)}
            </div>
            {s.files.length > 0 && <FileList files={s.files} />}
            <ResultNotes t={t} detail={s.detail} />
          </li>
        ))}
      </ul>
      <ResultNotes t={t} logError={report.logError} />
    </div>
  );
}
