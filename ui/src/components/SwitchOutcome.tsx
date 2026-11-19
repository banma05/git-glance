import type { ComponentChildren } from 'preact';
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

const whyOf = (t: T, s: Shelf) => (s.whyCode ? coded(t, 'shelf.why', s.whyCode, { branch: s.branch }) : '');

// Only these states leave work that is not back and will not come back on its
// own; the others either come back, or are already back.
const BY_HAND = new Set(['kept', 'superseded', 'orphan']);

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

// ByHand is the command that puts a shelf back by hand.
function ByHand({ t, hash }: { t: T; hash: string }) {
  return (
    <div class="by-hand">
      {t('switch.keptHow')} <code>{`git stash apply --index ${hash}`}</code>
    </div>
  );
}

// Kept says what happened to a shelf that was not put back.
function Kept({ t, s }: { t: T; s: Shelf }) {
  let line: ComponentChildren;
  if (s.whyCode === 'apply-partial') line = t('switch.partial', { branch: s.branch, hash: short(s.hash) });
  else if (s.state === 'pending') line = t('switch.keptPending', { branch: s.branch, why: whyOf(t, s) || coded(t, 'shelf.state', s.state, { branch: s.branch }) });
  else line = t('switch.kept', { branch: s.branch, why: whyOf(t, s) || coded(t, 'shelf.state', s.state, { branch: s.branch }), hash: short(s.hash) });
  return (
    <>
      <div class="bad">{line}</div>
      {BY_HAND.has(s.state) && <ByHand t={t} hash={s.hash} />}
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
      {r.shelved && <div>{t('switch.shelved', { n: r.shelved.files.length, branch: r.shelved.branch })}</div>}
      {r.restored && <div>{t('switch.restored', { n: r.restored.files.length, branch: r.restored.branch })}</div>}
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
      {BY_HAND.has(s.state) && <ByHand t={t} hash={s.hash} />}
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
