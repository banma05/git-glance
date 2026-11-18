import { useState } from 'preact/hooks';
import { rpc } from '../api';
import type { Host } from '../host/types';
import type { T } from '../i18n';
import { baseName, errorText } from '../reasons';

interface Props {
  t: T;
  host: Host;
  repos: string[];
  repo: string;
  onSelect(repo: string): void;
  onAdd(path: string): void;
}

export function RepoTabs({ t, host, repos, repo, onSelect, onAdd }: Props) {
  const [adding, setAdding] = useState(false);
  const [path, setPath] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  // The path may be any folder inside the repository: discover finds its root.
  const submit = async () => {
    const typed = path.trim();
    if (!typed || busy) return;
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
    <div class="tabs" role="tablist">
      {repos.map((r) => (
        <button class="tab" role="tab" aria-selected={r === repo} title={r} onClick={() => onSelect(r)}>
          {baseName(r)}
        </button>
      ))}
      <button class="tab add" title={t('repo.add')} aria-label={t('repo.add')} onClick={() => setAdding(!adding)}>
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
          <button class="btn" type="submit" disabled={busy || !path.trim()}>
            {t('repo.addSubmit')}
          </button>
          {error && (
            <span class="warn" role="alert">
              {error}
            </span>
          )}
        </form>
      )}
    </div>
  );
}
