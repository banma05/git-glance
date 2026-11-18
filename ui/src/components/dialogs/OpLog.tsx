import { useEffect, useState } from 'preact/hooks';
import { rpc } from '../../api';
import { dateTime } from '../../format';
import type { Host } from '../../host/types';
import type { Key, T } from '../../i18n';
import { en } from '../../i18n/en';
import { errorText, reasonText } from '../../reasons';
import type { ReasonKind } from '../../reasons';
import type { OpEntry } from '../../types';

const LIMIT = 200;
const RESULT_KIND: Record<string, ReasonKind> = {
  align: 'align.skip',
  create: 'create.reason',
  switch: 'switch.reason',
  'remove-folder': 'remove.reason',
};

function opText(op: string, t: T): string {
  const key = `oplog.op.${op}`;
  return key in en ? t(key as Key) : op;
}

// resultText writes "ok", a refusal reason, or a failure after a change began
// ("git-failed: <what git said>", "verify-failed: <what>", "links-failed: <error>") as text.
function resultText(e: OpEntry, t: T): string {
  if (e.result === 'ok') return t('oplog.result.ok');
  const kind = RESULT_KIND[e.op] ?? 'code';
  const m = /^(git-failed|verify-failed|links-failed): ?(.*)$/s.exec(e.result);
  if (m) return m[2] ? `${reasonText(kind, m[1], t)}: ${m[2]}` : reasonText(kind, m[1], t);
  return reasonText(kind, e.result, t);
}

export function OpLog({ t, host, repo }: { t: T; host: Host; repo: string }) {
  const [entries, setEntries] = useState<OpEntry[] | null>(null);
  const [error, setError] = useState<unknown>(null);
  useEffect(() => {
    rpc(host, 'ops', { repo, limit: LIMIT })
      .then((list) => setEntries([...list].sort((x, y) => y.time - x.time)))
      .catch(setError);
  }, [host, repo]);

  if (error) return <p class="warn">{errorText(error, t)}</p>;
  if (!entries) return <p class="muted">{t('app.loading')}</p>;
  if (entries.length === 0) return <p class="muted">{t('dialog.oplog.empty')}</p>;
  return (
    <table class="oplog">
      <thead>
        <tr>
          <th>{t('dialog.oplog.time')}</th>
          <th>{t('dialog.oplog.op')}</th>
          <th>{t('dialog.oplog.branch')}</th>
          <th>{t('dialog.oplog.folder')}</th>
          <th>{t('dialog.oplog.result')}</th>
          <th>{t('dialog.oplog.detail')}</th>
        </tr>
      </thead>
      <tbody>
        {entries.map((e) => (
          <tr>
            <td>{dateTime(e.time)}</td>
            <td>{opText(e.op, t)}</td>
            <td>{e.branch}</td>
            <td>{e.folder}</td>
            <td>{resultText(e, t)}</td>
            <td>{e.detail}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
