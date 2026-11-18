import type { T } from '../i18n';
import type { BranchView } from '../types';

// PLACEHOLDER (task 15b replaces this file with the full detail area).
export function DetailPanel({ t, branch }: { t: T; branch: BranchView | null }) {
  return <div class="detail placeholder">{branch ? branch.name : t('detail.none')}</div>;
}
