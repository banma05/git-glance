import { useEffect, useMemo } from 'preact/hooks';
import type { Host } from '../host/types';
import { makeT } from '../i18n';
import type { T } from '../i18n';
import { errorText } from '../reasons';
import { useApp } from '../state';
import { Dialogs } from './dialogs/Dialogs';
import { DetailPanel } from './DetailPanel';
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
  ) : null;
  const view = state.view;
  const errorLine = state.error ? (
    <div class="fetch-error" role="alert">
      <span>{errorText(state.error, t)}</span>
      {!view && state.repo && (
        <button class="btn" onClick={actions.reload}>
          {t('app.retry')}
        </button>
      )}
    </div>
  ) : null;
  const selected = view && state.selected ? (view.branches[state.selected] ?? null) : null;

  if (host.mode === 'sidebar') {
    return (
      <div class="app narrow">
        <Sidebar t={t} host={host} state={state} actions={actions} />
        {fetchLine}
        {errorLine}
        {!state.loaded && <div class="muted pad">{t('app.loading')}</div>}
        {state.loaded && state.repos.length === 0 && <div class="muted pad">{t('repo.none')}</div>}
        <Dialogs t={t} host={host} state={state} actions={actions} />
      </div>
    );
  }

  return (
    <div class="app wide">
      <RepoTabs t={t} host={host} repos={state.repos} repo={state.repo} onSelect={actions.selectRepo} onAdd={actions.addRepo} />
      <Toolbar
        t={t}
        actions={actions}
        mode={state.mode}
        busy={state.busy}
        editor={state.global?.editor ?? 'code'}
        theme={state.global?.theme ?? 'auto'}
        showEditor={!window.acquireVsCodeApi}
      />
      {fetchLine}
      {errorLine}
      {!state.loaded && <div class="muted pad">{t('app.loading')}</div>}
      {state.loaded && state.repos.length === 0 && <div class="muted pad">{t('repo.none')}</div>}
      {view && (
        <div class="main">
          {state.mode === 'graph' ? (
            <>
              <aside class="left">
                <ListView t={t} view={view} state={state} actions={actions} compact />
              </aside>
              <div class="center">
                <GraphView t={t} host={host} repo={state.repo} view={view} theme={state.theme} selected={state.selected} onSelect={actions.select} />
              </div>
            </>
          ) : (
            <div class="center">
              <ListView t={t} view={view} state={state} actions={actions} />
            </div>
          )}
        </div>
      )}
      {view && <DetailPanel t={t} branch={selected} state={state} actions={actions} />}
      <Dialogs t={t} host={host} state={state} actions={actions} />
    </div>
  );
}
