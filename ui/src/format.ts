import type { BranchView } from './types';
import type { Key, T } from './i18n';

export type Chip = { key: string; text: string; tone: 'ok' | 'warn' | 'info' | 'mute' };

const TEST_KEYS = { all: 'testAll', some: 'testSome', none: 'testNone' } as const;
const RELEASE_KEYS = { all: 'releaseAll', some: 'releaseSome', none: 'releaseNone' } as const;
const STATE_TONE = { all: 'ok', some: 'info', none: 'mute' } as const;

const BLOCK_KEYS: Record<string, Key> = {
  'not-in-folder': 'block.notInFolder',
  'main-folder': 'block.mainFolder',
  'folder-missing': 'block.folderMissing',
  dirty: 'block.dirty',
  unpushed: 'block.unpushed',
  'not-merged': 'block.notMerged',
};

// chips lists the status chips of a branch in display order.
export function chips(b: BranchView, t: T): Chip[] {
  const out: Chip[] = [];
  const add = (key: string, text: string, tone: Chip['tone']) => out.push({ key, text, tone });

  if (!b.hasUpstream) {
    if (b.unpushed > 0) add('unpushed', t('chip.unpushed'), 'mute');
  } else if (!b.upstreamGone) {
    if (b.behind > 0) add('behind', t('chip.behind', { n: b.behind }), 'warn');
    if (b.ahead > 0) add('ahead', t('chip.ahead', { n: b.ahead }), 'info');
    if (b.behind === 0 && b.ahead === 0) add('synced', t('chip.synced'), 'ok');
  }

  const changes = b.dirty ? b.dirty.changed + b.dirty.untracked : 0;
  if (changes > 0) add('dirty', t('chip.dirty', { n: changes }), 'warn');

  if (b.test) {
    const s = b.test.state;
    add(`test-${s}`, t(`chip.${TEST_KEYS[s]}`, { m: b.test.matched, n: b.test.total }), STATE_TONE[s]);
  }
  if (b.release) {
    const s = b.release.state;
    const m = b.release.in.length;
    add(`release-${s}`, t(`chip.${RELEASE_KEYS[s]}`, { m, n: m + b.release.notIn.length }), STATE_TONE[s]);
  }

  if (b.merged && b.canRemoveFolder) add('removable', t('chip.removable'), 'ok');
  if (b.folderMissing) add('folder-missing', t('chip.folderMissing'), 'warn');
  return out;
}

// blockText explains why a folder cannot be removed. An unknown reason from a
// newer core is shown as it is.
export function blockText(block: string, t: T): string {
  const key = BLOCK_KEYS[block];
  return key ? t(key) : block;
}

// shortDate writes a unix time (seconds) as MM-DD in local time; "" for an unset time.
export function shortDate(time: number): string {
  if (!time) return '';
  const d = new Date(time * 1000);
  const two = (n: number) => String(n).padStart(2, '0');
  return `${two(d.getMonth() + 1)}-${two(d.getDate())}`;
}
