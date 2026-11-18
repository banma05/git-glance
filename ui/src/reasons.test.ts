import { describe, expect, it } from 'vitest';
import { makeT } from './i18n';
import { errorText, reasonText } from './reasons';
import type { ReasonKind } from './reasons';
import { HostError } from './host/types';

const CODES: Record<ReasonKind, string[]> = {
  code: ['not-repo', 'bad-params', 'unknown-method', 'git-failed', 'not-allowed', 'editor-not-found', 'internal', 'no-token', 'bad-response', 'network'],
  'align.skip': ['dirty', 'ahead', 'diverged', 'upstream-gone', 'folder-missing', 'checked-out-twice', 'hidden-changes', 'git-failed'],
  'switch.reason': ['dirty', 'folder-missing', 'checked-out-elsewhere', 'no-branch', 'detached', 'hidden-changes'],
  'create.reason': ['bad-name', 'branch-exists', 'path-exists', 'path-inside-folder'],
  'remove.reason': [
    'not-in-folder', 'main-folder', 'folder-missing', 'dirty', 'unpushed', 'not-merged',
    'changed', 'hidden-changes', 'locked', 'backup-failed', 'git-failed',
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

  it('describes a host error by its code and message', () => {
    const t = makeT('en');
    expect(errorText(new HostError('git-failed', 'exit 1'), t)).toBe('A git command failed (exit 1)');
    expect(errorText(new Error('boom'), t)).toContain('boom');
  });
});
