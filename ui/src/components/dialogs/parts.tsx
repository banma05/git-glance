import type { T } from '../../i18n';

// Notes every result view shows when the core sent them; they are never dropped.
export function ResultNotes({ t, detail, logError }: { t: T; detail?: string; logError?: string }) {
  return (
    <>
      {detail ? <p class="muted">{t('dialog.detail', { detail })}</p> : null}
      {logError ? (
        <p class="warn" role="status">
          {t('detail.logError', { message: logError })}
        </p>
      ) : null}
    </>
  );
}

export function FileList({ files }: { files: string[] }) {
  return (
    <ul class="files">
      {files.map((f) => (
        <li>
          <code>{f}</code>
        </li>
      ))}
    </ul>
  );
}
