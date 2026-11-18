import { useRef, useState } from 'preact/hooks';
import { rpc } from '../../api';
import type { Host } from '../../host/types';
import type { T } from '../../i18n';
import { errorText } from '../../reasons';
import type { AppState } from '../../state';
import type { CommitInfo, CompareResult, FileDiffResult } from '../../types';

const COMMIT_CAP = 200; // the core lists at most this many commits per side

function Commits({ list }: { list: CommitInfo[] }) {
  return (
    <ul class="files">
      {list.map((c) => (
        <li>
          <code>{c.hash.slice(0, 7)}</code> {c.subject}
        </li>
      ))}
    </ul>
  );
}

export function Compare({ t, host, state }: { t: T; host: Host; state: AppState }) {
  const view = state.view;
  const names: string[] = [];
  if (view) {
    for (const g of view.groups) names.push(g.trunk.name);
    for (const n of Object.keys(view.branches)) if (!names.includes(n)) names.push(n);
  }
  const [a, setA] = useState(state.selected);
  const [b, setB] = useState('');
  const [shown, setShown] = useState<{ a: string; b: string; result: CompareResult } | null>(null);
  const [diff, setDiff] = useState<{ path: string; result: FileDiffResult } | null>(null);
  const [error, setError] = useState<unknown>(null);
  const seq = useRef(0);

  const run = () => {
    if (!a || !b) return;
    const mine = ++seq.current;
    setError(null);
    setDiff(null);
    rpc(host, 'compare', { repo: state.repo, a, b })
      .then((result) => {
        if (mine === seq.current) setShown({ a, b, result });
      })
      .catch((e) => {
        if (mine === seq.current) setError(e);
      });
  };

  const openFile = (path: string) => {
    if (!shown) return;
    const { a: fa, b: fb } = shown;
    const mine = ++seq.current;
    setError(null);
    host
      .openDiff(state.repo, fa, fb, path)
      .then((opened) => (opened ? null : rpc(host, 'fileDiff', { repo: state.repo, a: fa, b: fb, path })))
      .then((result) => {
        if (result && mine === seq.current) setDiff({ path, result });
      })
      .catch((e) => {
        if (mine === seq.current) setError(e);
      });
  };

  const options = (value: string) => [
    <option value="">{t('dialog.compare.pick')}</option>,
    ...names.map((n) => (
      <option value={n} selected={n === value}>
        {n}
      </option>
    )),
  ];
  const r = shown?.result ?? null;
  return (
    <div>
      <div class="form">
        <label class="field">
          {t('dialog.compare.a')}
          <select value={a} onChange={(e) => setA((e.target as HTMLSelectElement).value)}>
            {options(a)}
          </select>
        </label>
        <label class="field">
          {t('dialog.compare.b')}
          <select value={b} onChange={(e) => setB((e.target as HTMLSelectElement).value)}>
            {options(b)}
          </select>
        </label>
        <button class="btn" disabled={!a || !b || a === b} onClick={run}>
          {t('dialog.compare.run')}
        </button>
      </div>
      {error ? (
        <p class="warn" role="alert">
          {errorText(error, t)}
        </p>
      ) : null}
      {shown && r && (
        <div class="compare">
          {r.onlyA.length === 0 && r.onlyB.length === 0 && r.files.length === 0 && <p class="muted">{t('dialog.compare.same')}</p>}
          <div class="compare-sides">
            <div>
              <h4>{t('dialog.compare.onlyA', { name: shown.a })}</h4>
              <Commits list={r.onlyA} />
            </div>
            <div>
              <h4>{t('dialog.compare.onlyB', { name: shown.b })}</h4>
              <Commits list={r.onlyB} />
            </div>
          </div>
          {(r.onlyA.length >= COMMIT_CAP || r.onlyB.length >= COMMIT_CAP) && <p class="muted">{t('dialog.compare.commitsCut')}</p>}
          <h4>{t('dialog.compare.files')}</h4>
          <ul class="files">
            {r.files.map((f) => (
              <li>
                <button class="link" onClick={() => openFile(f.path)}>
                  <span class="status">{f.status}</span> <span>{f.path}</span>
                </button>
              </li>
            ))}
          </ul>
          {r.filesTruncated && <p class="muted">{t('dialog.compare.filesTruncated')}</p>}
        </div>
      )}
      {diff && (
        <div class="diff">
          <h4>{t('dialog.compare.diffOf', { path: diff.path })}</h4>
          {diff.result.diff ? <pre>{diff.result.diff}</pre> : <p class="muted">{t('dialog.compare.diffEmpty')}</p>}
          {diff.result.truncated && <p class="muted">{t('dialog.compare.diffTruncated')}</p>}
        </div>
      )}
    </div>
  );
}
