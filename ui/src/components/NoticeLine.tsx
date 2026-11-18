import { HostError } from '../host/types';
import type { T } from '../i18n';
import { errorText, reasonText } from '../reasons';
import type { ReasonKind } from '../reasons';
import type { Actions, Notice } from '../state';

const REASON_KIND: Record<string, ReasonKind> = { create: 'create.reason', remove: 'remove.reason' };

// NoticeLine reports the result of a write whose dialog was closed before it
// ended: which write, on what, and how it ended.
export function NoticeLine({ t, notice, onDismiss }: { t: T; notice: Notice; onDismiss: Actions['dismissNotice'] }) {
  const op = t(`notice.op.${notice.kind}`);
  const label = notice.target ? `${op} - ${notice.target}` : op;
  const reason = notice.error ? errorText(notice.error, t) : reasonText(REASON_KIND[notice.kind] ?? 'code', notice.reason, t);
  // a write that ran out of time on the client goes on in the core: not "not done"
  const running = notice.error instanceof HostError && notice.error.code === 'still-running';
  const text = notice.ok ? t('notice.ok', { label }) : running ? t('notice.running', { label, reason }) : t('notice.failed', { label, reason });
  return (
    <div class={notice.ok || running ? 'fetch-note' : 'fetch-error'} role="status" data-testid="notice">
      <span>{text}</span>
      {notice.logError && <span>{t('detail.logError', { message: notice.logError })}</span>}
      <button class="btn" onClick={onDismiss}>
        {t('notice.dismiss')}
      </button>
    </div>
  );
}
