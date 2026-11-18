import { useEffect, useState } from 'preact/hooks';
import { rpc } from '../../api';
import { formatBytes } from '../../format';
import type { Host } from '../../host/types';
import type { T } from '../../i18n';
import { errorText } from '../../reasons';
import type { Actions, AppState } from '../../state';
import type { BackupsInfo, Theme } from '../../types';
import { EDITORS } from '../Toolbar';
import { useGuard } from './parts';

type Role = 'trunk' | 'test' | 'release' | 'none';
const ROLES: Role[] = ['trunk', 'test', 'release', 'none'];
const THEMES: Theme[] = ['auto', 'dark', 'light'];

interface Props {
  t: T;
  host: Host;
  state: AppState;
  actions: Pick<Actions, 'refreshView' | 'setEditor' | 'setTheme'>;
}

type Note = { ok: boolean; text: string } | null;

export function Settings({ t, host, state, actions }: Props) {
  const view = state.view;
  const repo = state.repo;
  const guard = useGuard();
  const trunks = view ? view.groups.map((g) => g.trunk.name) : [];

  // branch roles
  const [branch, setBranch] = useState('');
  const [role, setRole] = useState<Role>('test');
  const [trunk, setTrunk] = useState(trunks[0] ?? '');
  const [remote, setRemote] = useState<string[] | null>(null);
  const [roleNote, setRoleNote] = useState<Note>(null);

  // hidden prefix and old-branch days
  const [prefix, setPrefix] = useState(view?.settings.hidePrefix ?? '');
  const [days, setDays] = useState(String(view?.settings.oldDays ?? 30));
  const [viewNote, setViewNote] = useState<Note>(null);

  // backups
  const [backups, setBackups] = useState<BackupsInfo | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [backupNote, setBackupNote] = useState<Note>(null);

  const loadBackups = () =>
    rpc(host, 'backups')
      .then(setBackups)
      .catch(() => setBackups(null));
  useEffect(() => {
    void loadBackups();
  }, [host]);

  const searchRemote = () => {
    rpc(host, 'remoteBranches', { repo, query: branch.trim() })
      .then(setRemote)
      .catch((e) => setRoleNote({ ok: false, text: errorText(e, t) }));
  };

  const applyRole = () => {
    const name = branch.trim();
    if (!name) return;
    const params = role === 'test' || role === 'release' ? { repo, branch: name, role, trunk } : { repo, branch: name, role };
    guard.run(async () => {
      try {
        await rpc(host, 'setRole', params);
        setRoleNote({ ok: true, text: t('dialog.settings.saved') });
        actions.refreshView();
      } catch (e) {
        setRoleNote({ ok: false, text: errorText(e, t) });
      }
    });
  };

  const daysNumber = Number(days);
  const daysOk = days.trim() !== '' && Number.isInteger(daysNumber);
  const saveView = () => {
    if (!daysOk) return;
    guard.run(async () => {
      try {
        await rpc(host, 'setRepoSettings', { repo, hidePrefix: prefix, oldDays: daysNumber });
        setViewNote({ ok: true, text: t('dialog.settings.saved') });
        actions.refreshView();
      } catch (e) {
        setViewNote({ ok: false, text: errorText(e, t) });
      }
    });
  };

  const clearBackups = () => {
    guard.run(async () => {
      try {
        await rpc(host, 'clearBackups');
        setBackupNote({ ok: true, text: t('dialog.settings.cleared') });
      } catch (e) {
        setBackupNote({ ok: false, text: errorText(e, t) });
      }
      setConfirming(false);
      await loadBackups();
    });
  };

  const noteView = (n: Note) => (n ? <p class={n.ok ? 'ok' : 'warn'}>{n.text}</p> : null);
  const choices = [...(view ? Object.keys(view.branches) : []), ...(remote ?? [])];
  return (
    <div class="settings">
      <h4>{t('dialog.settings.roles')}</h4>
      <div class="form">
        <label class="field">
          {t('dialog.settings.branch')}
          <input type="text" list="gg-branch-choices" value={branch} onInput={(e) => setBranch((e.target as HTMLInputElement).value)} />
        </label>
        <datalist id="gg-branch-choices">
          {choices.map((n) => (
            <option value={n} />
          ))}
        </datalist>
        <button class="btn" onClick={searchRemote}>
          {t('dialog.settings.searchRemote')}
        </button>
        {remote && remote.length === 0 && <span class="muted">{t('dialog.settings.remoteNone')}</span>}
        <label class="field">
          {t('dialog.settings.role')}
          <select value={role} onChange={(e) => setRole((e.target as HTMLSelectElement).value as Role)}>
            {ROLES.map((r) => (
              <option value={r}>{t(`dialog.settings.role.${r}`)}</option>
            ))}
          </select>
        </label>
        {(role === 'test' || role === 'release') && (
          <label class="field">
            {t('dialog.settings.trunkOf')}
            <select value={trunk} onChange={(e) => setTrunk((e.target as HTMLSelectElement).value)}>
              {trunks.map((n) => (
                <option value={n}>{n}</option>
              ))}
            </select>
          </label>
        )}
        <button class="btn" disabled={guard.busy || !branch.trim()} onClick={applyRole}>
          {t('dialog.settings.apply')}
        </button>
        {noteView(roleNote)}
      </div>

      <h4>{t('dialog.settings.view')}</h4>
      <div class="form">
        <label class="field">
          {t('dialog.settings.hidePrefix')}
          <input type="text" value={prefix} onInput={(e) => setPrefix((e.target as HTMLInputElement).value)} />
        </label>
        <label class="field">
          {t('dialog.settings.oldDays')}
          <input type="number" min="1" max="3650" value={days} onInput={(e) => setDays((e.target as HTMLInputElement).value)} />
        </label>
        <button class="btn" disabled={guard.busy || !daysOk} onClick={saveView}>
          {t('dialog.settings.save')}
        </button>
        {noteView(viewNote)}
        {!window.acquireVsCodeApi && (
          <label class="field">
            {t('toolbar.openWith')}
            <select value={state.global?.editor ?? 'code'} onChange={(e) => actions.setEditor((e.target as HTMLSelectElement).value)}>
              {EDITORS.map((id) => (
                <option value={id}>{id}</option>
              ))}
            </select>
          </label>
        )}
        <label class="field">
          {t('dialog.settings.theme')}
          <select value={state.global?.theme ?? 'auto'} onChange={(e) => actions.setTheme((e.target as HTMLSelectElement).value as Theme)}>
            {THEMES.map((th) => (
              <option value={th}>{t(`dialog.settings.theme.${th}`)}</option>
            ))}
          </select>
        </label>
      </div>

      <h4>{t('dialog.settings.backups')}</h4>
      {backups && <p>{t('dialog.settings.backupUsage', { size: formatBytes(backups.bytes), dir: backups.dir })}</p>}
      {confirming ? (
        <div class="confirm" role="alertdialog">
          <p class="warn">{t('dialog.settings.clearWarn')}</p>
          <button class="btn danger" disabled={guard.busy} onClick={clearBackups}>
            {t('dialog.settings.clearYes')}
          </button>{' '}
          <button class="btn" disabled={guard.busy} onClick={() => setConfirming(false)}>
            {t('app.cancel')}
          </button>
        </div>
      ) : (
        <button class="btn" disabled={guard.busy} onClick={() => setConfirming(true)}>
          {t('dialog.settings.clearBackups')}
        </button>
      )}
      {noteView(backupNote)}
    </div>
  );
}
