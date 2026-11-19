import type { ComponentChildren } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import { rpc } from '../api';
import { dateTime } from '../format';
import type { Host } from '../host/types';
import type { T } from '../i18n';
import { errorText } from '../reasons';
import type { CommitFiles, CommitInfo, FileChange, FileDiffResult, FileText, TrunkNews as News } from '../types';

// What the list is about: the trunk commits a branch lacks, or the latest
// commits of a trunk on its own. Refs are full, as the compare dialog sends them.
export type NewsSource =
  | { kind: 'news'; trunk: string; branch: string; trunkName: string; branchName: string }
  | { kind: 'recent'; ref: string; trunkName: string };

export const NEWS_LIST_CAP = 200; // the core lists at most this many trunk commits a branch lacks
const RECENT = 50; // the latest commits listed for a trunk on its own

type Listed = { status: 'loading' } | { status: 'ok'; commits: CommitInfo[]; news: News | null } | { status: 'error'; error: unknown };
type Opened = { status: 'loading' } | { status: 'ok'; files: CommitFiles } | { status: 'error'; error: unknown };
// What the page shows of one file: its diff, the text a root commit added, or why neither.
type Body = { kind: 'diff'; diff: FileDiffResult } | { kind: 'added'; text: FileText } | { kind: 'error'; error: unknown };

interface Props {
  t: T;
  host: Host;
  repo: string;
  source: NewsSource;
  fetchedAt: number; // the last read of the remote (unix seconds), 0 when unknown
  onClose(): void;
}

const short = (hash: string) => hash.slice(0, 7);

// TrunkNews lists commits newest first; a commit opens to the files it changed
// (against its first parent) and a file to its diff. For a branch, "Combined"
// shows the files the trunk changed since the branch started. It only reads:
// every call it makes is a read of the core, and nothing is fetched here.
export function TrunkNews({ t, host, repo, source, fetchedAt, onClose }: Props) {
  const [listed, setListed] = useState<Listed>({ status: 'loading' });
  const [combined, setCombined] = useState(false);
  const [opened, setOpened] = useState<Record<string, Opened>>({}); // by commit hash
  const [expanded, setExpanded] = useState<string[]>([]);
  const [shown, setShown] = useState<{ key: string; body: Body } | null>(null);
  const gen = useRef(0); // answers to an older read are dropped
  const key = source.kind === 'news' ? `${repo}|${source.trunk}|${source.branch}` : `${repo}|${source.ref}`;

  const load = () => {
    const mine = ++gen.current;
    setListed({ status: 'loading' });
    setOpened({});
    setExpanded([]);
    setShown(null);
    const read: Promise<{ commits: CommitInfo[]; news: News | null }> =
      source.kind === 'news'
        ? rpc(host, 'trunkNews', { repo, trunk: source.trunk, branch: source.branch }).then((news) => ({ commits: news.commits, news }))
        : rpc(host, 'recentCommits', { repo, ref: source.ref, limit: RECENT }).then((commits) => ({ commits, news: null }));
    read.then(
      (r) => mine === gen.current && setListed({ status: 'ok', ...r }),
      (error: unknown) => mine === gen.current && setListed({ status: 'error', error }),
    );
  };
  // read again when the list is about something else, or the remote was read again
  useEffect(load, [key, fetchedAt]);

  const toggle = (hash: string) => {
    if (expanded.includes(hash)) {
      setExpanded(expanded.filter((h) => h !== hash));
      return;
    }
    setExpanded([...expanded, hash]);
    if (opened[hash]?.status === 'ok' || opened[hash]?.status === 'loading') return;
    const mine = gen.current;
    setOpened((o) => ({ ...o, [hash]: { status: 'loading' } }));
    rpc(host, 'commitFiles', { repo, hash }).then(
      (files) => mine === gen.current && setOpened((o) => ({ ...o, [hash]: { status: 'ok', files } })),
      (error: unknown) => mine === gen.current && setOpened((o) => ({ ...o, [hash]: { status: 'error', error } })),
    );
  };

  // openFile shows the change of one file from a to b: in the editor when the
  // host can, else in the page. A root commit has no a: its file is all new.
  const openFile = (fileKey: string, a: string, b: string, path: string) => {
    if (shown?.key === fileKey) {
      setShown(null);
      return;
    }
    const mine = gen.current;
    const put = (body: Body) => mine === gen.current && setShown({ key: fileKey, body });
    setShown(null);
    if (!a) {
      rpc(host, 'fileAt', { repo, ref: b, path }).then(
        (text) => put({ kind: 'added', text }),
        (error: unknown) => put({ kind: 'error', error }),
      );
      return;
    }
    host
      .openDiff(repo, a, b, path)
      .then((done) => (done ? null : rpc(host, 'fileDiff', { repo, a, b, path })))
      .then((diff) => diff && put({ kind: 'diff', diff }))
      .catch((error: unknown) => put({ kind: 'error', error }));
  };

  const files = (prefix: string, list: FileChange[], a: string, b: string) =>
    list.length === 0 ? (
      <p class="muted">{t('news.noFiles')}</p>
    ) : (
      <ul class="files">
        {list.map((f) => {
          const fileKey = `${prefix}|${f.path}`;
          return (
            <li key={fileKey}>
              <button class="link" onClick={() => openFile(fileKey, a, b, f.path)}>
                <span class="status">{f.status}</span> <span>{f.path}</span>
              </button>
              {shown?.key === fileKey && <FileBody t={t} body={shown.body} />}
            </li>
          );
        })}
      </ul>
    );

  const title = source.kind === 'news' ? t('news.titleNews', { trunk: source.trunkName, branch: source.branchName }) : t('news.titleRecent', { trunk: source.trunkName });
  return (
    <section class="news" aria-label={title}>
      <div class="news-head">
        <strong class="news-title">{title}</strong>
        <span class="news-tools">
          <button class="btn" onClick={load}>
            {t('news.refresh')}
          </button>
          <button class="btn" onClick={onClose}>
            {t('app.close')}
          </button>
        </span>
      </div>
      <div class="muted news-asof">{fetchedAt > 0 ? t('news.asOf', { time: dateTime(fetchedAt) }) : t('news.asOfUnknown')}</div>
      {source.kind === 'news' && (
        <div class="seg news-modes" role="group">
          <button class="btn" aria-pressed={!combined} onClick={() => setCombined(false)}>
            {t('news.byCommit')}
          </button>
          <button class="btn" aria-pressed={combined} onClick={() => setCombined(true)}>
            {t('news.combined')}
          </button>
        </div>
      )}
      {listed.status === 'loading' && <p class="muted">{t('graph.loading')}</p>}
      {listed.status === 'error' && (
        <p class="warn" role="alert">
          {t('graph.loadFailed', { reason: errorText(listed.error, t) })}{' '}
          <button class="btn" onClick={load}>
            {t('app.retry')}
          </button>
        </p>
      )}
      {listed.status === 'ok' &&
        (combined && listed.news ? (
          <Combined t={t} news={listed.news} files={files} />
        ) : (
          <>
            <p class="muted news-count">{countText(t, source, listed)}</p>
            <ul class="news-list">
              {listed.commits.map((c) => {
                const open = expanded.includes(c.hash);
                const state = opened[c.hash];
                return (
                  <li key={c.hash}>
                    <button class="news-commit" aria-expanded={open} onClick={() => toggle(c.hash)}>
                      <span class="news-line">
                        <code>{short(c.hash)}</code> {c.subject}
                      </span>
                      <span class="muted news-meta">
                        {c.author} · {dateTime(c.time)}
                      </span>
                    </button>
                    {open && (
                      <div class="news-body">
                        {(!state || state.status === 'loading') && <p class="muted">{t('graph.loading')}</p>}
                        {state?.status === 'error' && <p class="warn">{t('graph.loadFailed', { reason: errorText(state.error, t) })}</p>}
                        {state?.status === 'ok' && (
                          <>
                            {files(c.hash, state.files.files, state.files.parent, c.hash)}
                            {state.files.truncated && <p class="muted">{t('dialog.compare.filesTruncated')}</p>}
                          </>
                        )}
                      </div>
                    )}
                  </li>
                );
              })}
            </ul>
          </>
        ))}
    </section>
  );
}

function countText(t: T, source: NewsSource, listed: Extract<Listed, { status: 'ok' }>): string {
  const shownCount = listed.commits.length;
  if (source.kind === 'recent') return shownCount === 0 ? t('news.empty') : t('news.recentCount', { n: shownCount });
  const total = listed.news?.total ?? shownCount;
  if (total === 0) return t('news.none');
  return total > shownCount ? t('news.countCut', { total, shown: shownCount }) : t('news.count', { n: total });
}

interface CombinedProps {
  t: T;
  news: News;
  files(prefix: string, list: FileChange[], a: string, b: string): ComponentChildren;
}

// Combined is what the trunk changed since the branch started: the files from
// the fork point to the trunk tip, never the branch's own changes.
function Combined({ t, news, files }: CombinedProps) {
  if (!news.base) return <p class="muted news-count">{t('news.noBase')}</p>;
  return (
    <>
      <p class="muted news-count">{t('news.combinedNote', { base: short(news.base), tip: short(news.tip) })}</p>
      {files('combined', news.files, news.base, news.tip)}
      {news.filesTruncated && <p class="muted">{t('dialog.compare.filesTruncated')}</p>}
    </>
  );
}

function FileBody({ t, body }: { t: T; body: Body }) {
  if (body.kind === 'error') return <p class="warn">{errorText(body.error, t)}</p>;
  if (body.kind === 'added') {
    if (body.text.binary) return <p class="muted">{t('news.binary')}</p>;
    return (
      <>
        <p class="muted">{t('news.rootFile')}</p>
        <pre>{body.text.text}</pre>
        {body.text.truncated && <p class="muted">{t('news.textCut')}</p>}
      </>
    );
  }
  return (
    <>
      {body.diff.diff ? <pre>{body.diff.diff}</pre> : <p class="muted">{t('dialog.compare.diffEmpty')}</p>}
      {body.diff.truncated && <p class="muted">{t('dialog.compare.diffTruncated')}</p>}
    </>
  );
}
