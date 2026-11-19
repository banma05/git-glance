import { dateTime } from '../format';
import type { Host } from '../host/types';
import type { T } from '../i18n';
import type { Actions, AppState, NewsTarget } from '../state';
import type { Group, TrunkConfig, TrunkView, View } from '../types';
import { DetailPanel } from './DetailPanel';
import { SwitchOutcome } from './SwitchOutcome';
import { trunkRef } from './dialogs/Compare';
import { TrunkNews } from './TrunkNews';
import type { NewsSource } from './TrunkNews';

interface Props {
  t: T;
  host: Host;
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

// localText is how the local branch of a trunk stands against its remote copy.
function localText(tv: TrunkView, t: T): string {
  if (!tv.hasLocal) return t('trunk.local.none');
  if (tv.ref === tv.name) return t('trunk.local.only');
  const parts: string[] = [];
  if (tv.localAhead > 0) parts.push(t('trunk.local.ahead', { n: tv.localAhead }));
  if (tv.localBehind > 0) parts.push(t('trunk.local.behind', { n: tv.localBehind }));
  return parts.length > 0 ? parts.join(' · ') : t('trunk.local.same');
}

interface TrunkDetailProps {
  t: T;
  group: Group;
  config?: TrunkConfig; // the trunk's roles as the settings name them; none for the default trunk
  newsOpen?: boolean; // its recent commits are listed
  locked?: boolean; // no change can start now
  switched?: AppState['switchResult']; // the last switch, shown when it went to this trunk or its test branch
  actions: Pick<Actions, 'openFolder' | 'copy' | 'openDialog' | 'toggleNews' | 'switchTo'>;
}

// remoteOf is the remote a trunk is compared through ("origin" for
// origin/main), "" when it has no remote copy.
function remoteOf(tv: TrunkView): string {
  return tv.ref !== tv.name && tv.ref.endsWith('/' + tv.name) ? tv.ref.slice(0, -(tv.name.length + 1)) : '';
}

// TrunkDetail is what a click on a trunk's name shows: what it is compared
// through, its latest commit, its local branch, its roles and its branches.
// It only reads; none of its buttons changes anything.
export function TrunkDetail({ t, group, config, newsOpen = false, locked = false, switched = null, actions }: TrunkDetailProps) {
  const tv = group.trunk;
  const notSet = <span class="muted">{t('trunk.notSet')}</span>;
  // The roles as set: the view keeps only the branches it found, so a role
  // whose branch is gone is shown as not found, not as not set.
  const test = config ? config.test : tv.test;
  const releases = config ? config.releases : tv.releases;
  const role = (name: string, found: boolean) => (found ? name : <span class="warn">{t('trunk.notFound', { name })}</span>);
  return (
    <div class="detail">
      <div class="detail-name">
        <strong>{tv.name}</strong>
        <span class="muted">{t('trunk.title')}</span>
      </div>
      <dl class="detail-grid trunk-grid">
        <dt>{t('trunk.ref')}</dt>
        <dd>{tv.ref === tv.name ? t('trunk.refLocal', { name: tv.name }) : tv.ref}</dd>

        <dt>{t('trunk.latest', { ref: tv.ref })}</dt>
        <dd>
          <code>{tv.hash.slice(0, 7)}</code>
          {tv.time > 0 && (
            <span class="muted">
              {' · '}
              <span class="when">{dateTime(tv.time)}</span>
            </span>
          )}
        </dd>

        <dt>{t('trunk.local')}</dt>
        <dd>{localText(tv, t)}</dd>

        {tv.folder && (
          <>
            <dt>{t('detail.folder')}</dt>
            <dd>{tv.folder}</dd>
          </>
        )}

        <dt>{t('trunk.test')}</dt>
        <dd>{test ? role(test, tv.test === test) : notSet}</dd>

        <dt>{t('trunk.releases')}</dt>
        <dd>
          {releases.length > 0
            ? releases.map((n, i) => (
                <>
                  {i > 0 && ', '}
                  {role(n, tv.releases.includes(n))}
                </>
              ))
            : notSet}
        </dd>

        <dt>{t('trunk.branches')}</dt>
        <dd>{group.branches.length}</dd>
      </dl>
      {(!test || releases.length === 0) && (
        <p class="muted hint">{t('trunk.rolesHint', { settings: t('toolbar.settings'), roles: t('dialog.settings.roles') })}</p>
      )}
      <div class="detail-actions">
        {[tv.name, ...(tv.test ? [tv.test] : [])].map((branch) => (
          <button class="btn" disabled={locked} onClick={() => actions.switchTo(branch, { latest: true, remote: remoteOf(tv) })}>
            {t('switch.latest', { branch })}
          </button>
        ))}
        {tv.folder && (
          <button class="btn" onClick={() => actions.openFolder(tv.folder)}>
            {t('detail.open')}
          </button>
        )}
        <button class="btn" onClick={() => actions.copy(tv.name)}>
          {t('detail.copy')}
        </button>
        <button class="btn" onClick={() => actions.openDialog('compare')}>
          {t('detail.compare')}
        </button>
        <button class="btn" aria-pressed={newsOpen} onClick={() => actions.toggleNews({ kind: 'trunk', trunk: tv.name })}>
          {t('news.recent')}
        </button>
      </div>
      {switched && !switched.back && (switched.branch === tv.name || switched.branch === tv.test) && (
        <SwitchOutcome t={t} branch={switched.branch} result={switched.result} inPlace={switched.inPlace} />
      )}
    </div>
  );
}

// newsSource is what the open news list reads: the trunk commits a branch
// lacks (through the trunk's compared ref), or a trunk's recent commits.
export function newsSource(view: View, news: NewsTarget | null): NewsSource | null {
  if (news?.kind === 'branch') {
    const b = view.branches[news.branch];
    const g = b ? view.groups.find((x) => x.trunk.name === b.trunk) : undefined;
    if (!b || !g) return null;
    return { kind: 'news', trunk: trunkRef(g.trunk), branch: `refs/heads/${b.name}`, trunkName: g.trunk.name, branchName: b.name };
  }
  if (news?.kind === 'trunk') {
    const g = view.groups.find((x) => x.trunk.name === news.trunk);
    return g ? { kind: 'recent', ref: trunkRef(g.trunk), trunkName: g.trunk.name } : null;
  }
  return null;
}

// NewsPanel is the open trunk news list, under the details it belongs to.
export function NewsPanel({ t, host, view, state, actions }: Props) {
  const source = newsSource(view, state.news);
  if (!source || !state.news) return null;
  const news = state.news;
  return <TrunkNews t={t} host={host} repo={state.repo} source={source} fetchedAt={view.fetchedAt} onClose={() => actions.toggleNews(news)} />;
}

// assignOf is what the "belongs to trunk" control of a branch needs.
export function assignOf(view: View, branch: { name: string } | null): { trunks: string[]; current: string } | undefined {
  if (!branch) return undefined;
  return { trunks: view.groups.map((g) => g.trunk.name), current: view.settings.manualTrunk[branch.name] ?? '' };
}

// Details shows what the selection is about: a trunk, a trunk commit, or a
// branch (with the picked commit when there is one), and under it the open
// trunk news list.
export function Details(props: Props) {
  return (
    <>
      <Selection {...props} />
      <NewsPanel {...props} />
    </>
  );
}

function Selection({ t, view, state, actions }: Props) {
  const group = state.selectedTrunk ? view.groups.find((g) => g.trunk.name === state.selectedTrunk) : undefined;
  if (group) {
    const config = view.settings.trunks.find((c) => c.name === group.trunk.name);
    const newsOpen = state.news?.kind === 'trunk' && state.news.trunk === group.trunk.name;
    const locked = state.busy || !!state.gitProblem;
    return <TrunkDetail t={t} group={group} config={config} newsOpen={newsOpen} locked={locked} switched={state.switchResult} actions={actions} />;
  }
  if (state.pick?.kind === 'trunk') return <TrunkCommitDetail t={t} view={view} pick={state.pick} />;
  const branch = state.selected ? (view.branches[state.selected] ?? null) : null;
  return <DetailPanel t={t} branch={branch} state={state} actions={actions} pick={state.pick} assign={assignOf(view, branch)} />;
}
