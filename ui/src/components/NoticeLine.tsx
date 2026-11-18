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
  return (
    <div class={notice.ok ? 'fetch-note' : 'fetch-error'} role="status" data-testid="notice">
      <span>{notice.ok ? t('notice.ok', { label }) : t('notice.failed', { label, reason })}</span>
      {notice.logError && <span>{t('detail.logError', { message: notice.logError })}</span>}
      <button class="btn" onClick={onDismiss}>
        {t('notice.dismiss')}
      </button>
    </div>
  );
}
