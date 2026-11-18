import { describe, expect, it } from 'vitest';
import { makeT } from './i18n';
import { errorText, reasonText } from './reasons';
import type { ReasonKind } from './reasons';
import { HostError } from './host/types';

const CODES: Record<ReasonKind, string[]> = {
  code: [
    'not-repo', 'bad-params', 'unknown-method', 'git-failed', 'not-allowed', 'editor-not-found', 'internal', 'no-token', 'bad-response', 'network',
    'git-too-old', 'bare-repo', 'unsafe-repo',
    // made up by the VS Code shell
    'timeout', 'still-running', 'core-exited', 'core-unavailable', 'core-disposed', 'bad-path', 'bad-message', 'error',
  ],
  'align.skip': [
    'dirty', 'ahead', 'diverged', 'upstream-gone', 'folder-missing', 'checked-out-twice', 'hidden-changes', 'git-failed',
    'in-progress', 'tracks-other-branch', 'verify-failed',
  ],
  'switch.reason': ['dirty', 'folder-missing', 'checked-out-elsewhere', 'no-branch', 'detached', 'hidden-changes', 'git-failed', 'verify-failed'],
  'create.reason': [
    'bad-name', 'branch-exists', 'path-exists', 'path-inside-folder',
    'no-branch', 'checked-out-elsewhere', 'in-progress', 'git-failed', 'verify-failed',
  ],
  'remove.reason': [
    'not-in-folder', 'main-folder', 'folder-missing', 'dirty', 'unpushed', 'not-merged',
    'changed', 'hidden-changes', 'locked', 'backup-failed', 'git-failed',
    'nested-repo', 'orphaned-commits', 'locked-worktree', 'error', 'in-progress', 'folder-is-link', 'links-failed',
  ],
};

describe('reason texts', () => {
  for (const locale of ['zh-cn', 'en']) {
    it(`cover every code in ${locale}`, () => {
      const t = makeT(locale);
      for (const [kind, codes] of Object.entries(CODES) as [ReasonKind, string[]][]) {
        for (const code of codes) expect(reasonText(kind, code, t), `${kind}.${code}`).not.toBe(code);
      }
    });
  }

  it('shows an unknown code as it is', () => {
    expect(reasonText('code', 'from-the-future', makeT('en'))).toBe('from-the-future');
  });

  it('says a write that ran out of time is still running', () => {
    const text = errorText(new HostError('still-running', 'removeFolder is still running after 30 min'), makeT('en'));
    expect(text).toMatch(/still running/i);
    expect(text).toMatch(/operation log/i);
  });

  it('describes a host error by its code and message', () => {
    const t = makeT('en');
    expect(errorText(new HostError('git-failed', 'exit 1'), t)).toBe('A git command failed (exit 1)');
    expect(errorText(new Error('boom'), t)).toContain('boom');
  });
});
