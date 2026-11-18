import type { T } from '../i18n';
import { baseName } from '../reasons';

interface Props {
  t: T;
  repos: string[];
  repo: string;
  onSelect(repo: string): void;
  onAdd(path: string): void;
}

export function RepoTabs({ t, repos, repo, onSelect, onAdd }: Props) {
  const add = () => {
    const path = window.prompt(t('repo.addPrompt'));
    if (path && path.trim()) onAdd(path.trim());
  };
  return (
    <div class="tabs" role="tablist">
      {repos.map((r) => (
        <button
          class="tab"
          role="tab"
          aria-selected={r === repo}
          title={r}
          onClick={() => onSelect(r)}
        >
          {baseName(r)}
        </button>
      ))}
      <button class="tab add" title={t('repo.add')} aria-label={t('repo.add')} onClick={add}>
        +
      </button>
    </div>
  );
}
