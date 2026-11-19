import { useRef, useState } from 'preact/hooks';
import { rpc } from '../../api';
import type { Host } from '../../host/types';
import type { T } from '../../i18n';
import { errorText } from '../../reasons';
import type { AppState } from '../../state';
import type { CommitInfo, CompareResult, FileDiffResult, TrunkView } from '../../types';

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

// trunkRef is the full ref a trunk is compared through: its remote copy when
// it has one (design 4.1), else the local branch.
export function trunkRef(tv: TrunkView): string {
  return tv.ref === tv.name ? `refs/heads/${tv.name}` : `refs/remotes/${tv.ref}`;
}

type Shown = { a: string; b: string; refA: string; refB: string; result: CompareResult };

export function Compare({ t, host, state }: { t: T; host: Host; state: AppState }) {
  const view = state.view;
  // name shown in the lists -> the full ref sent to the core, so that a tag or a
  // remote branch of the same name can never be meant instead
  const refs = new Map<string, string>();
  if (view) {
    for (const g of view.groups) refs.set(g.trunk.name, trunkRef(g.trunk));
    for (const n of Object.keys(view.branches)) if (!refs.has(n)) refs.set(n, `refs/heads/${n}`);
  }
  const names = [...refs.keys()];
  const [a, setA] = useState(state.selected || state.selectedTrunk); // what the details were about
  const [b, setB] = useState('');
  const [shown, setShown] = useState<Shown | null>(null);
  const [diff, setDiff] = useState<{ path: string; result: FileDiffResult } | null>(null);
  const [error, setError] = useState<unknown>(null);
  const seq = useRef(0);

  const run = () => {
    const refA = refs.get(a);
    const refB = refs.get(b);
    if (!refA || !refB) return;
    const mine = ++seq.current;
    setError(null);
    setDiff(null);
    rpc(host, 'compare', { repo: state.repo, a: refA, b: refB })
      .then((result) => {
        if (mine === seq.current) setShown({ a, b, refA, refB, result });
      })
      .catch((e) => {
        if (mine === seq.current) setError(e);
      });
  };

  const openFile = (path: string) => {
    if (!shown) return;
    const { refA: fa, refB: fb } = shown;
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
