import type { Key, T } from '../i18n';
import { en } from '../i18n/en';
import { reasonText } from '../reasons';
import type { Actions, AppState } from '../state';
import { inside, mainFolderOf } from '../state';
import type { Shelf, SwitchResult } from '../types';

const short = (hash: string) => hash.slice(0, 7);

// coded is the text of a code the core sends under prefix, or the generic text
// of that prefix for a code from a newer core.
function coded(t: T, prefix: 'shelf.why' | 'shelf.state' | 'switch.noff', code: string, vars: Record<string, string | number> = {}): string {
  const key = `${prefix}.${code}`;
  return key in en ? t(key as Key, vars) : t(`${prefix}.other` as Key, { ...vars, code });
}

// takenPaths are the paths a path-exists detail of the core names ("…already
// on disk (a, b); putting…"), or "" when it names none.
function takenPaths(s: Shelf): string {
  const open = 'on disk (';
  const start = s.why.indexOf(open);
  const end = s.why.lastIndexOf('); ');
  return s.whyCode === 'path-exists' && start >= 0 && end > start ? s.why.slice(start + open.length, end) : '';
}

function whyOf(t: T, s: Shelf): string {
  const paths = takenPaths(s);
  if (paths) return t('shelf.takenPaths', { paths });
  return s.whyCode ? coded(t, 'shelf.why', s.whyCode, { branch: s.branch }) : '';
}

// What to do next about a shelf that is not back in the folder:
//   apply: it is in git stash list and nothing of it is in the folder;
//   clear: as apply, but files sit where it would go back; git would stop on
//          them or write over them, so they are moved first;
//   check: part or all of it may already be in the folder, so look first;
//   gone:  it is no longer listed, but git may still have its commit;
//   none:  it comes back on its own, it is already back, or this page does not
//          know enough to say (a state or reason of a newer core).
type Next = 'apply' | 'clear' | 'check' | 'gone' | 'none';

// Only these states leave work that is not back and will not come back on its own.
const NOT_AUTOMATIC = new Set(['kept', 'superseded', 'orphan']);
// The reasons after which the work is whole in git stash list and none of it is in the folder.
const WHOLE_IN_STASH = new Set(['', 'apply-failed', 'base-moved', 'not-on-branch', 'folder-dirty', 'superseded', 'no-record']);

function nextOf(s: Shelf): Next {
  if (!NOT_AUTOMATIC.has(s.state)) return 'none';
  if (s.whyCode === 'apply-partial' || s.whyCode === 'apply-mismatch') return 'check';
  if (s.whyCode === 'entry-gone') return 'gone';
  if (s.whyCode === 'path-exists') return 'clear';
  return WHOLE_IN_STASH.has(s.whyCode) ? 'apply' : 'none';
}

// English is the core's own detail, folded away; the line above says it in the page's language.
function English({ t, text }: { t: T; text: string }) {
  if (!text) return null;
  return (
    <details class="core-detail">
      <summary>{t('shelf.more')}</summary>
      {text}
    </details>
  );
}

// NextStep is the command for the next step about a shelf that is not back, if there is one.
function NextStep({ t, s }: { t: T; s: Shelf }) {
  const next = nextOf(s);
  if (next === 'none') return null;
  const labels: Record<Exclude<Next, 'none'>, string> = {
    apply: t('switch.keptHow'),
    clear: t('switch.clearFirst'), // the files in the way go first, then the command
    check: t('switch.checkFirst'),
    gone: t('switch.goneHow'),
  };
  const label = labels[next];
  const command = next === 'check' ? `git stash show -p ${s.hash}` : `git stash apply --index ${s.hash}`;
  return (
    <div class="by-hand">
      {label} <code>{command}</code>
    </div>
  );
}

// keptLine says what is true of a shelf that was not put back.
function keptLine(t: T, s: Shelf): string {
  const vars = { branch: s.branch, hash: short(s.hash), why: whyOf(t, s) || coded(t, 'shelf.state', s.state, { branch: s.branch }) };
  if (s.state === 'pending') return t('switch.keptPending', vars);
  const next = nextOf(s);
  if (next === 'check') return t(s.whyCode === 'apply-partial' ? 'switch.partial' : 'switch.mismatch', vars);
  if (next === 'gone') return t('switch.gone', vars);
  if (next === 'clear') {
    const paths = takenPaths(s);
    return paths ? t('switch.inTheWay', { ...vars, paths }) : t('switch.inTheWayUnnamed', vars);
  }
  if (next === 'apply') return t('switch.kept', vars);
  return t('switch.keptOther', vars);
}

// Kept says what happened to a shelf that was not put back.
function Kept({ t, s }: { t: T; s: Shelf }) {
  return (
    <>
      <div class="bad">{keptLine(t, s)}</div>
      <NextStep t={t} s={s} />
      <English t={t} text={s.why} />
    </>
  );
}

interface OutcomeProps {
  t: T;
  branch: string; // where the switch went
  result: SwitchResult;
  inPlace?: boolean; // refused dirty while the folder was already on the branch
  stale?: boolean; // the remote could not be read before a switch to the latest
}

// SwitchOutcome says what a switch of the main folder did: switched or why
// not, brought up to date or not, and what happened to uncommitted work.
export function SwitchOutcome({ t, branch, result: r, inPlace = false, stale = false }: OutcomeProps) {
  let head = t('switch.done', { branch });
  if (!r.ok) head = inPlace ? t('switch.dirtyInPlace', { branch }) : reasonText('switch.reason', r.reason, t);
  return (
    <div class="switch-result" role="status">
      <div class={r.ok ? 'ok' : 'bad'}>{head}</div>
      {r.ok && stale && <div class="bad">{t('switch.staleRemote', { branch })}</div>}
      {r.fastForwarded && <div>{t('switch.fastForwarded')}</div>}
      {r.noFastForward && <div>{coded(t, 'switch.noff', r.noFastForward)}</div>}
      <English t={t} text={r.aheadNote} />
      {r.ok && r.shelved?.state === 'pending' && <div>{t('switch.shelved', { n: r.shelved.files.length, branch: r.shelved.branch })}</div>}
      {!r.ok && r.shelved && r.restored && <div>{t('switch.shelvedBack', { n: r.shelved.files.length, branch: r.shelved.branch })}</div>}
      {r.ok && r.restored && <div>{t('switch.restored', { n: r.restored.files.length, branch: r.restored.branch })}</div>}
      {r.restored && r.restored.state !== 'restored' && (
        <>
          <div class="muted">{t('switch.entryKept', { why: whyOf(t, r.restored) || coded(t, 'shelf.state', r.restored.state) })}</div>
          <English t={t} text={r.restored.why} />
        </>
      )}
      {r.keptShelf && <Kept t={t} s={r.keptShelf} />}
      {!r.ok && r.path && <div class="muted">{t('detail.path', { path: r.path })}</div>}
      {r.files.length > 0 && (
        <ul class="files">
          {r.files.map((f) => (
            <li>{f}</li>
          ))}
        </ul>
      )}
      {r.detail && <div class="muted">{t('dialog.detail', { detail: r.detail })}</div>}
      {r.logError && <div class="bad">{t('detail.logError', { message: r.logError })}</div>}
    </div>
  );
}

// ShelfRow is one shelf in git stash list as the main-folder line lists it.
function ShelfRow({ t, s }: { t: T; s: Shelf }) {
  const head =
    s.state === 'orphan'
      ? t('main.orphan', { branch: s.branch, hash: short(s.hash) })
      : t('main.shelf', { n: s.files.length, branch: s.branch, hash: short(s.hash) });
  const state = coded(t, 'shelf.state', s.state, { branch: s.branch });
  const why = s.state === 'orphan' ? '' : whyOf(t, s); // an orphan's only reason is that it has no record
  return (
    <div class="shelf-row">
      <div>{head}</div>
      <div class="muted">{why ? t('shelf.stateWhy', { state, why }) : state}</div>
      <NextStep t={t} s={s} />
      <English t={t} text={s.why} />
    </div>
  );
}

interface LineProps {
  t: T;
  state: Pick<AppState, 'view' | 'repo' | 'switchState' | 'switchResult' | 'busy' | 'gitProblem'>;
  actions: Pick<Actions, 'switchBack'>;
}

// MainFolderLine is about the repository's main folder: the branch it was
// switched away from with the control that switches back, the work shelved
// for it (and Git Glance entries found in git stash list without a record),
// and what the last switch back did.
export function MainFolderLine({ t, state, actions }: LineProps) {
  if (!state.view) return null;
  const main = mainFolderOf(state.view, state.repo).folder;
  const previous = state.switchState?.previous ?? '';
  const mine = (s: Shelf) => s.state === 'orphan' || (inside(s.folder, main) && inside(main, s.folder));
  const shelves = (state.switchState?.shelves ?? []).filter(mine);
  const back = state.switchResult?.back ? state.switchResult : null;
  if (!previous && shelves.length === 0 && !back) return null;
  return (
    <div class="main-folder">
      {previous && (
        <div class="main-away">
          <span>{t('main.away', { branch: previous })}</span>
          <button class="btn" disabled={state.busy || !!state.gitProblem} onClick={actions.switchBack}>
            {t('main.back', { branch: previous })}
          </button>
        </div>
      )}
      {shelves.map((s) => (
        <ShelfRow key={s.hash} t={t} s={s} />
      ))}
      {back && <SwitchOutcome t={t} branch={back.branch} result={back.result} inPlace={back.inPlace} stale={back.stale} />}
    </div>
  );
}
