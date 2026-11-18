import { dateTime } from '../format';
import type { T } from '../i18n';
import type { Actions, AppState } from '../state';
import type { View } from '../types';
import { DetailPanel } from './DetailPanel';

interface Props {
  t: T;
  view: View;
  state: AppState;
  actions: Actions;
}

// TrunkCommitDetail is what a click on a trunk dot shows: the commit and the
// branches that start from it.
export function TrunkCommitDetail({ t, view, pick }: { t: T; view: View; pick: Extract<NonNullable<AppState['pick']>, { kind: 'trunk' }> }) {
  const forks = Object.values(view.branches).filter((b) => b.trunk === pick.trunk && b.forkHash === pick.hash);
  return (
    <div class="detail">
      <div class="detail-name">
        <strong>{t('detail.trunkCommit')}</strong>
      </div>
      <dl class="detail-grid">
        <dt>{t('detail.trunk')}</dt>
        <dd>{pick.trunk}</dd>
        <dt>{t('detail.hash')}</dt>
        <dd>
          <code>{pick.hash}</code>
        </dd>
        {pick.subject && (
          <>
            <dt>{t('detail.subject')}</dt>
            <dd>{pick.subject}</dd>
          </>
        )}
        {pick.time > 0 && (
          <>
            <dt>{t('detail.date')}</dt>
            <dd>{dateTime(pick.time)}</dd>
          </>
        )}
        <dt>{t('detail.forksHere')}</dt>
        <dd>
          {forks.length === 0 ? (
            <span class="muted">{t('detail.noForks')}</span>
          ) : (
            <ul class="files">
              {forks.map((b) => (
                <li>{b.name}</li>
              ))}
            </ul>
          )}
        </dd>
      </dl>
    </div>
  );
}

// Details shows what the selection is about: a trunk commit, or a branch
// (with the picked commit when there is one).
export function Details({ t, view, state, actions }: Props) {
  if (state.pick?.kind === 'trunk') return <TrunkCommitDetail t={t} view={view} pick={state.pick} />;
  const branch = state.selected ? (view.branches[state.selected] ?? null) : null;
  return <DetailPanel t={t} branch={branch} state={state} actions={actions} pick={state.pick} />;
}
