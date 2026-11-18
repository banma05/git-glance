import { useEffect, useRef, useState } from 'preact/hooks';
import { rpc } from '../../api';
import type { Host } from '../../host/types';
import type { T } from '../../i18n';
import { errorText, reasonText } from '../../reasons';
import type { Actions, AppState } from '../../state';
import { FileList, ResultNotes } from './parts';

interface Props {
  t: T;
  host: Host;
  state: AppState;
  actions: Pick<Actions, 'createFolder'>;
  existing?: boolean; // a folder for the branch selected when the dialog opened, not a new branch
}

// CreateFolder makes a requirement folder: a new branch from a trunk, or with
// `existing` a folder for an existing local branch that has none.
export function CreateFolder({ t, host, state, actions, existing = false }: Props) {
  const view = state.view;
  const trunks = view ? view.groups.map((g) => g.trunk.name) : [];
  const selectedTrunk = view && state.selected ? (view.branches[state.selected]?.trunk ?? '') : '';
  const [trunk, setTrunk] = useState(trunks.includes(selectedTrunk) ? selectedTrunk : (trunks[0] ?? ''));
  const [branch, setBranch] = useState(() => (existing ? state.selected : ''));
  const [repo] = useState(state.repo); // the repository the dialog was opened for
  const moved = repo !== state.repo;
  const [path, setPath] = useState('');
  const edited = useRef(false); // the user typed in the path: stop replacing it
  const seq = useRef(0);

  const name = branch.trim();
  useEffect(() => {
    if (!name || !repo) return;
    const mine = ++seq.current;
    rpc(host, 'defaultFolder', { repo, branch: name })
      .then((r) => {
        if (mine === seq.current && !edited.current) setPath(r.path);
      })
      .catch(() => {
        // a name the core rejects shows its reason when the folder is created
      });
  }, [name, repo, host]);

  const out = existing && state.createOutcome?.branch !== name ? null : state.createOutcome;
  const ready = (existing || !!trunk) && !!name && !!path.trim() && !moved;
  return (
    <form
      class="form"
      onSubmit={(e) => {
        e.preventDefault();
        if (ready && !state.busy && !state.gitProblem) actions.createFolder(repo, trunk, name, path.trim(), existing);
      }}
    >
      {moved && (
        <p class="warn" role="status">
          {t('dialog.repoChanged')}
        </p>
      )}
      {existing ? (
        <p>{t('dialog.createExisting.branch', { branch: name })}</p>
      ) : (
        <>
          {trunks.length === 0 && <p class="muted">{t('dialog.create.noTrunk')}</p>}
          <label class="field">
            {t('dialog.create.trunk')}
            <select value={trunk} onChange={(e) => setTrunk((e.target as HTMLSelectElement).value)}>
              {trunks.map((n) => (
                <option value={n}>{n}</option>
              ))}
            </select>
          </label>
          <label class="field">
            {t('dialog.create.branch')}
            <input type="text" value={branch} onInput={(e) => setBranch((e.target as HTMLInputElement).value)} />
          </label>
        </>
      )}
      <label class="field">
        {t('dialog.create.path')}
        <input
          type="text"
          value={path}
          onInput={(e) => {
            edited.current = true;
            setPath((e.target as HTMLInputElement).value);
          }}
        />
      </label>
      <div class="dialog-actions">
        <button class="btn" type="submit" disabled={!ready || state.busy || !!state.gitProblem}>
          {t('dialog.create.submit')}
        </button>
      </div>
      {out && (
        <div class="outcome" role="status">
          {out.error ? (
            <p class="warn">{errorText(out.error, t)}</p>
          ) : out.result?.ok ? (
            <>
              <p class="ok">{t('dialog.create.done')}</p>
              <p>{t('detail.path', { path: out.result.path })}</p>
            </>
          ) : (
            <>
              <p class="warn">{t('dialog.create.refused', { reason: reasonText('create.reason', out.result?.reason ?? '', t) })}</p>
              {out.result?.path && <p>{t('detail.path', { path: out.result.path })}</p>}
              {out.result && out.result.files.length > 0 && <FileList files={out.result.files} />}
            </>
          )}
          <ResultNotes t={t} detail={out.result?.detail} logError={out.result?.logError} />
        </div>
      )}
    </form>
  );
}
