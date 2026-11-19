import type { T } from '../i18n';
import { reasonText } from '../reasons';
import type { Actions, AppState } from '../state';
import { inside, mainFolderOf } from '../state';
import type { SwitchResult } from '../types';

const short = (hash: string) => hash.slice(0, 7);

interface OutcomeProps {
  t: T;
  branch: string; // where the switch went
  result: SwitchResult;
  inPlace?: boolean; // refused dirty while the folder was already on the branch
}

// SwitchOutcome says what a switch of the main folder did: switched or why
// not, brought up to date or not, and what happened to uncommitted work.
export function SwitchOutcome({ t, branch, result: r, inPlace = false }: OutcomeProps) {
  let head = t('switch.done', { branch });
  if (!r.ok) head = inPlace ? t('switch.dirtyInPlace', { branch }) : reasonText('switch.reason', r.reason, t);
  return (
    <div class="switch-result" role="status">
      <div class={r.ok ? 'ok' : 'bad'}>{head}</div>
      {r.fastForwarded && <div>{t('switch.fastForwarded')}</div>}
      {r.aheadNote && (
        <div>
          {t('switch.ahead')} <span class="muted">({r.aheadNote})</span>
        </div>
      )}
      {r.shelved && <div>{t('switch.shelved', { n: r.shelved.files.length, branch: r.shelved.branch })}</div>}
      {r.restored && <div>{t('switch.restored', { n: r.restored.files.length, branch: r.restored.branch })}</div>}
      {r.restored?.why && <div class="muted">{t('switch.restoredNote', { why: r.restored.why })}</div>}
      {r.keptShelf && <div class="bad">{t('switch.kept', { branch: r.keptShelf.branch, hash: short(r.keptShelf.hash) })}</div>}
      {r.keptShelf?.why && <div class="muted">{r.keptShelf.why}</div>}
      {r.keptShelf && (
        <div>
          {t('switch.keptHow')} <code>{`git stash apply --index ${r.keptShelf.hash}`}</code>
        </div>
      )}
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

interface LineProps {
  t: T;
  state: Pick<AppState, 'view' | 'repo' | 'switchState' | 'switchResult' | 'busy' | 'gitProblem'>;
  actions: Pick<Actions, 'switchBack'>;
}

// MainFolderLine is about the repository's main folder: the branch it was
// switched away from with the control that switches back, the uncommitted
// work kept in git stash list, and what the last switch back did.
export function MainFolderLine({ t, state, actions }: LineProps) {
  if (!state.view) return null;
  const main = mainFolderOf(state.view, state.repo).folder;
  const previous = state.switchState?.previous ?? '';
  const shelves = (state.switchState?.shelves ?? []).filter((s) => inside(s.folder, main) && inside(main, s.folder));
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
        <div class="muted" key={s.hash}>
          {t('main.shelf', { n: s.files.length, branch: s.branch, hash: short(s.hash) })}
          {s.why && <div>{t('main.shelfStuck', { why: s.why })}</div>}
        </div>
      ))}
      {back && <SwitchOutcome t={t} branch={back.branch} result={back.result} inPlace={back.inPlace} />}
    </div>
  );
}
