import { useState } from 'preact/hooks';
import { rpc } from '../api';
import type { Host } from '../host/types';
import type { T } from '../i18n';
import { baseName, errorText } from '../reasons';

interface AddProps {
  t: T;
  host: Host;
  readOnly: boolean; // the core refuses every change, adding a repository too
  buttonClass: string;
  onAdd(path: string): void;
}

// AddRepo is the "+" and the form it opens. The path may be any folder inside
// the repository: discover finds its root, then onAdd adds it.
export function AddRepo({ t, host, readOnly, buttonClass, onAdd }: AddProps) {
  const [adding, setAdding] = useState(false);
  const [path, setPath] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const submit = async () => {
    const typed = path.trim();
    if (!typed || busy || readOnly) return;
    setBusy(true);
    setError('');
    try {
      const found = await rpc(host, 'discover', { path: typed });
      onAdd(found.repo);
      setAdding(false);
      setPath('');
    } catch (e) {
      setError(errorText(e, t));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <button class={buttonClass} title={t('repo.add')} aria-label={t('repo.add')} aria-expanded={adding} onClick={() => setAdding(!adding)}>
        +
      </button>
      {adding && (
        <form
          class="add-repo"
          onSubmit={(e) => {
            e.preventDefault();
            void submit();
          }}
        >
          <label class="field">
            {t('repo.addPrompt')}
            <input type="text" value={path} onInput={(e) => setPath((e.target as HTMLInputElement).value)} />
          </label>
          <button class="btn" type="submit" disabled={busy || readOnly || !path.trim()}>
            {t('repo.addSubmit')}
          </button>
          {error && (
            <span class="warn" role="alert">
              {error}
            </span>
          )}
        </form>
      )}
    </>
  );
}

interface Props {
  t: T;
  host: Host;
  repos: string[];
  repo: string;
  readOnly?: boolean;
  onSelect(repo: string): void;
  onAdd(path: string): void;
}

export function RepoTabs({ t, host, repos, repo, readOnly = false, onSelect, onAdd }: Props) {
  return (
    <div class="tabs" role="tablist">
      {repos.map((r) => (
        <button class="tab" role="tab" aria-selected={r === repo} title={r} onClick={() => onSelect(r)}>
          {baseName(r)}
        </button>
      ))}
      <AddRepo t={t} host={host} readOnly={readOnly} buttonClass="tab add" onAdd={onAdd} />
    </div>
  );
}
