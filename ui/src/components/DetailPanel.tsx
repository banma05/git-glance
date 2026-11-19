import { useEffect, useState } from 'preact/hooks';
import { blockText, chips, dateTime } from '../format';
import type { T } from '../i18n';
import { errorText, reasonText } from '../reasons';
import type { Actions, AppState, Picked } from '../state';
import type { BranchView, OpResult } from '../types';
import { NEWS_LIST_CAP } from './TrunkNews';

interface Props {
  t: T;
  branch: BranchView | null;
  state: Pick<AppState, 'busy' | 'switchResult'> & Partial<Pick<AppState, 'gitProblem' | 'assignResult' | 'news'>>;
  pick?: Picked | null; // a commit of this branch picked in the graph
  actions: Pick<Actions, 'switchTo' | 'openFolder' | 'copy' | 'openDialog' | 'assign' | 'toggleNews'>;
  assign?: { trunks: string[]; current: string }; // the trunks, and the one this branch was put under by hand ("" none)
}

const SOURCE_KEYS = { created: 'detail.source.created', manual: 'detail.source.manual' } as const;
const BY_KEYS = {
  ancestor: 'detail.by.ancestor',
  patch: 'detail.by.patch',
  subject: 'detail.by.subject',
  squash: 'detail.by.squash',
  '': 'detail.by.none',
} as const;
const REMOTE_CHIPS = ['unpushed', 'behind', 'ahead', 'synced'];

// A source code from a newer core that has no text yet is shown as it is.
function sourceNote(source: string, t: T): string {
  if (source === 'count') return '';
  return source in SOURCE_KEYS ? t(SOURCE_KEYS[source as keyof typeof SOURCE_KEYS]) : source;
}

function remoteText(b: BranchView, t: T): string {
  if (b.hasUpstream && b.upstreamGone) return t('detail.remote.gone');
  const parts = chips(b, t)
    .filter((c) => REMOTE_CHIPS.includes(c.key))
    .map((c) => c.text);
  if (parts.length === 0 && !b.hasUpstream) return t('chip.unpushed');
  return parts.join(' · ');
}

function SwitchResult({ t, result }: { t: T; result: OpResult }) {
  return (
    <div class="switch-result" role="status">
      <div class={result.ok ? 'ok' : 'bad'}>{result.ok ? t('detail.switched') : reasonText('switch.reason', result.reason, t)}</div>
      {result.path && <div class="muted">{t('detail.path', { path: result.path })}</div>}
      {result.files.length > 0 && (
        <ul class="files">
          {result.files.map((f) => (
            <li>{f}</li>
          ))}
        </ul>
      )}
      {result.detail && <div class="muted">{t('dialog.detail', { detail: result.detail })}</div>}
      {result.logError && <div class="bad">{t('detail.logError', { message: result.logError })}</div>}
    </div>
  );
}

interface AssignProps {
  t: T;
  branch: string;
  trunks: string[];
  current: string;
  locked: boolean;
  error: unknown;
  onApply(trunk: string): void;
}

// AssignTrunk puts a branch under a trunk by hand, or back to being decided by
// its commits. Nothing is saved until Apply.
function AssignTrunk({ t, branch, trunks, current, locked, error, onApply }: AssignProps) {
  const [pick, setPick] = useState(current);
  useEffect(() => setPick(current), [current, branch]);
  return (
    <div class="assign">
      <label class="field">
        {t('detail.assign')}
        <select value={pick} onChange={(e) => setPick((e.target as HTMLSelectElement).value)}>
          <option value="">{t('detail.assignAuto')}</option>
          {trunks.map((n) => (
            <option value={n}>{n}</option>
          ))}
        </select>
      </label>
      <button class="btn" disabled={locked || pick === current} onClick={() => onApply(pick)}>
        {t('detail.assignApply')}
      </button>
      {error ? <span class="warn">{t('detail.assignFailed', { reason: errorText(error, t) })}</span> : null}
    </div>
  );
}

// DetailPanel is everything about the selected branch and what can be done with
// it. It never pushes, pulls or merges; removing a folder only opens the
// confirm dialog.
export function DetailPanel({ t, branch, state, actions, pick = null, assign }: Props) {
  if (!branch) return <div class="detail placeholder">{t('detail.none')}</div>;
  const b = branch;
  const result = state.switchResult && state.switchResult.branch === b.name ? state.switchResult.result : null;
  const dirty = b.dirty;
  const hidden = dirty ? dirty.changed + dirty.untracked - dirty.files.length : 0;
  const note = sourceNote(b.trunkSource, t);
  const commit = pick?.kind === 'commit' && pick.branch === b.name ? b.own.find((c) => c.hash === pick.hash) : undefined;
  const locked = state.busy || !!state.gitProblem; // no change can start now

  return (
    <div class="detail">
      <div class="detail-name">
        <strong title={t('detail.name')}>{b.name}</strong>
      </div>
      {b.error && (
        <p class="warn" role="status">
          {t('detail.error', { message: b.error })}
        </p>
      )}
      {commit && (
        <dl class="detail-grid detail-commit">
          <dt>{t('detail.commit')}</dt>
          <dd>
            <code>{commit.hash}</code>
          </dd>
          <dt>{t('detail.subject')}</dt>
          <dd>{commit.subject}</dd>
          <dt>{t('detail.date')}</dt>
          <dd>{dateTime(commit.time)}</dd>
          {b.test && (
            <>
              <dt>{t('detail.test')}</dt>
              <dd>{byText(b.test.per.find((c) => c.hash === commit.hash)?.by ?? '', t)}</dd>
            </>
          )}
          {b.release && (
            <>
              <dt>{t('detail.release')}</dt>
              <dd>
                {b.release.in.length > 0 && <div>{t('detail.releaseIn', { list: b.release.in.join(', ') })}</div>}
                {b.release.notIn.length > 0 && <div>{t('detail.releaseNotIn', { list: b.release.notIn.join(', ') })}</div>}
              </dd>
            </>
          )}
        </dl>
      )}
      <dl class="detail-grid">
        <dt>{t('detail.folder')}</dt>
        <dd>{b.folder ? b.folder : t('detail.noFolder')}</dd>

        <dt>{t('detail.trunk')}</dt>
        <dd>{b.trunk ? (note ? `${b.trunk} (${note})` : b.trunk) : t('detail.trunkUnknown')}</dd>

        <dt>{t('detail.remote')}</dt>
        <dd>{remoteText(b, t)}</dd>

        <dt>{t('detail.chips')}</dt>
        <dd>
          {chips(b, t).map((c) => (
            <span class={`chip ${c.tone}`}>{c.text}</span>
          ))}
        </dd>

        {b.lastCommit > 0 && (
          <>
            <dt>{t('detail.lastCommit')}</dt>
            <dd>{dateTime(b.lastCommit)}</dd>
          </>
        )}

        {dirty && (
          <>
            <dt>{t('detail.files')}</dt>
            <dd>
              <ul class="files">
                {dirty.files.map((f) => (
                  <li>{f}</li>
                ))}
              </ul>
              {hidden > 0 && <div class="muted">{t('detail.moreFiles', { n: hidden })}</div>}
            </dd>
          </>
        )}

        {b.test && (
          <>
            <dt>{t('detail.test')}</dt>
            <dd>
              <ul class="files">
                {b.test.per.map((c) => (
                  <li>
                    <code>{c.hash.slice(0, 7)}</code> {c.subject} <span class="muted">({byText(c.by, t)})</span>
                  </li>
                ))}
              </ul>
            </dd>
          </>
        )}

        {b.release && (
          <>
            <dt>{t('detail.release')}</dt>
            <dd>
              {b.release.in.length > 0 && <div>{t('detail.releaseIn', { list: b.release.in.join(', ') })}</div>}
              {b.release.notIn.length > 0 && <div>{t('detail.releaseNotIn', { list: b.release.notIn.join(', ') })}</div>}
            </dd>
          </>
        )}
      </dl>

      {assign && assign.trunks.length > 0 && (
        <AssignTrunk
          t={t}
          branch={b.name}
          trunks={assign.trunks}
          current={assign.current}
          locked={locked}
          error={state.assignResult?.branch === b.name ? state.assignResult.error : null}
          onApply={(trunk) => actions.assign(b.name, trunk)}
        />
      )}
      <div class="detail-actions">
        {b.folder ? (
          <button class="btn" onClick={() => actions.openFolder(b.folder)}>
            {t('detail.open')}
          </button>
        ) : (
          <>
            <button class="btn" disabled={locked} onClick={() => actions.switchTo(b.name)}>
              {t('detail.switch')}
            </button>
            <button class="btn" disabled={locked} onClick={() => actions.openDialog('createExisting')}>
              {t('detail.create')}
            </button>
          </>
        )}
        <button class="btn" onClick={() => actions.copy(b.name)}>
          {t('detail.copy')}
        </button>
        <button class="btn" onClick={() => actions.openDialog('compare')}>
          {t('detail.compare')}
        </button>
        {b.trunk && (
          <button
            class="btn"
            aria-pressed={state.news?.kind === 'branch' && state.news.branch === b.name}
            onClick={() => actions.toggleNews({ kind: 'branch', branch: b.name })}
          >
            {t('news.open', { n: b.behindTrunk > NEWS_LIST_CAP ? `${NEWS_LIST_CAP}+` : b.behindTrunk })}
          </button>
        )}
        <button class="btn" disabled={!b.canRemoveFolder || locked} onClick={() => actions.openDialog('remove')}>
          {b.canRemoveFolder ? t('detail.remove') : t('detail.removeBlocked', { reason: blockText(b.removeBlock, t) })}
        </button>
      </div>
      {result && <SwitchResult t={t} result={result} />}
    </div>
  );
}

function byText(by: string, t: T): string {
  return by in BY_KEYS ? t(BY_KEYS[by as keyof typeof BY_KEYS]) : by;
}
