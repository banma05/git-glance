import { useEffect, useMemo } from 'preact/hooks';
import type { Host } from '../host/types';
import { makeT } from '../i18n';
import type { T } from '../i18n';
import { errorText } from '../reasons';
import { HostError } from '../host/types';
import { useApp } from '../state';
import type { AppState, GitProblem } from '../state';
import { Dialogs } from './dialogs/Dialogs';
import { NoticeLine } from './NoticeLine';
import { Details } from './Details';
import { GraphView } from './GraphView';
import { ListView } from './ListView';
import { RepoTabs } from './RepoTabs';
import { Sidebar } from './Sidebar';
import { Toolbar } from './Toolbar';

function NoToken({ t }: { t: T }) {
  return (
    <div class="fullpage" role="alert">
      <h2>{t('error.noToken')}</h2>
      <p>{t('error.noTokenHint')}</p>
    </div>
  );
}

// GitLine says why nothing can be changed: the core needs git 2.30 or newer.
function GitLine({ t, problem }: { t: T; problem: GitProblem }) {
  const text =
    problem.kind === 'old' ? t('git.tooOld', { version: problem.version }) : problem.kind === 'missing' ? t('git.missing') : t('git.tooOldUnknown');
  return (
    <div class="fetch-error" role="alert">
      <span>{text}</span>
    </div>
  );
}

// Status lists what the page knows about its repository list: loading, the
// workspace folders the editor could not add, or that the list is empty.
// "No repository yet" shows only when the list answered empty and nothing
// else explains it.
function Status({ t, state }: { t: T; state: AppState }) {
  if (!state.loaded) return <div class="muted pad">{t('app.loading')}</div>;
  const problems = state.discoveryProblems.map((p) => {
    const reason = errorText(new HostError(p.code, p.message), t);
    return (
      <div class="fetch-error" role="alert">
        <span>{p.folder ? t('discovery.failed', { folder: p.folder, reason }) : t('discovery.failedAll', { reason })}</span>
      </div>
    );
  });
  if (problems.length > 0) return <>{problems}</>;
  if (!state.listed || state.repos.length > 0) return null;
  return <div class="muted pad">{t(state.gitProblem ? 'repo.noneNoGit' : 'repo.none')}</div>;
}

export function App({ host }: { host: Host }) {
  const { state, actions } = useApp(host);
  const t = useMemo(() => makeT(host.locale), [host]);

  useEffect(() => {
    document.documentElement.dataset.theme = state.theme;
  }, [state.theme]);

  if (state.noToken) return <NoToken t={t} />;

  const fetchLine = state.fetchError ? (
    <div class="fetch-error" role="status">
      <span>{t('fetch.failed', { reason: state.fetchError })}</span>
      <button class="btn" disabled={state.fetching} onClick={actions.refresh}>
        {t('app.retry')}
      </button>
    </div>
  ) : state.fetching ? (
    <div class="fetch-note muted">{t('fetch.running')}</div>
  ) : state.fetchSlow ? (
    <div class="fetch-note muted" role="status">
      <span>{t('fetch.slow')}</span>
      <button class="btn" onClick={actions.refresh}>
        {t('app.retry')}
      </button>
    </div>
  ) : null;
  const view = state.view;
  const errorLine = state.error ? (
    <div class="fetch-error" role="alert">
      <span>{errorText(state.error, t)}</span>
      {!view && (state.repo || !state.listed) && (
        <button class="btn" onClick={state.repo ? actions.reload : actions.reloadRepos}>
          {t('app.retry')}
        </button>
      )}
    </div>
  ) : null;
  const noticeLine = state.notice ? <NoticeLine t={t} notice={state.notice} onDismiss={actions.dismissNotice} /> : null;
  const gitLine = state.gitProblem ? <GitLine t={t} problem={state.gitProblem} /> : null;

  if (host.mode === 'sidebar') {
    return (
      <div class="app narrow">
        <Sidebar t={t} host={host} state={state} actions={actions}>
          {gitLine}
          {fetchLine}
          {errorLine}
          {noticeLine}
          <Status t={t} state={state} />
        </Sidebar>
        <Dialogs t={t} host={host} state={state} actions={actions} />
      </div>
    );
  }

  return (
    <div class="app wide">
      <RepoTabs
        t={t}
        host={host}
        repos={state.repos}
        repo={state.repo}
        readOnly={!!state.gitProblem}
        onSelect={actions.selectRepo}
        onAdd={actions.addRepo}
      />
      <Toolbar
        t={t}
        actions={actions}
        mode={state.mode}
        busy={state.busy}
        readOnly={!!state.gitProblem}
        editor={state.global?.editor ?? 'code'}
        theme={state.global?.theme ?? 'auto'}
        showEditor={!window.acquireVsCodeApi}
      />
      {gitLine}
      {fetchLine}
      {errorLine}
      {noticeLine}
      <Status t={t} state={state} />
      {view && (
        <div class="main">
          {state.mode === 'graph' ? (
            <>
              <aside class="left">
                <ListView t={t} view={view} state={state} actions={actions} compact />
              </aside>
              <div class="center">
                <GraphView t={t} host={host} repo={state.repo} view={view} theme={state.theme} selected={state.selected} pick={state.pick} onSelect={actions.select} onCommit={actions.selectCommit} onTrunk={actions.selectTrunk} trunk={state.selectedTrunk} onShowTrunk={actions.showTrunk} />
              </div>
            </>
          ) : (
            <div class="center">
              <ListView t={t} view={view} state={state} actions={actions} />
            </div>
          )}
        </div>
      )}
      {view && (
        <div class="details-area">
          <Details t={t} host={host} view={view} state={state} actions={actions} />
        </div>
      )}
      <Dialogs t={t} host={host} state={state} actions={actions} />
    </div>
  );
}
