import { useEffect, useRef, useState } from 'preact/hooks';
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

// useGuard runs one write at a time: a second start while one runs is ignored
// at once (before any re-render), and `busy` disables the buttons meanwhile.
export function useGuard(): { busy: boolean; run(fn: () => Promise<void>): void } {
  const running = useRef(false);
  const alive = useRef(true);
  const [busy, setBusy] = useState(false);
  useEffect(
    () => () => {
      alive.current = false;
    },
    [],
  );
  const run = (fn: () => Promise<void>) => {
    if (running.current) return;
    running.current = true;
    setBusy(true);
    void fn().finally(() => {
      running.current = false;
      if (alive.current) setBusy(false);
    });
  };
  return { busy, run };
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
