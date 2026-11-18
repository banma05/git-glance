import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/preact';
import { Toolbar } from './Toolbar';
import { makeT } from '../i18n';
import type { Actions } from '../state';

const FORBIDDEN = /push|pull|sync|merge|rebase|推送|拉取|同步|合并|变基/i;

function actions(): Actions {
  return new Proxy({} as Actions, { get: () => vi.fn() });
}

afterEach(cleanup);

describe('wide toolbar', () => {
  for (const locale of ['zh-cn', 'en']) {
    it(`has no push, pull, sync, merge or rebase button in ${locale}`, () => {
      const t = makeT(locale);
      render(<Toolbar t={t} actions={actions()} mode="list" busy={false} editor="code" theme="auto" showEditor />);
      const buttons = screen.getAllByRole('button');
      expect(buttons.length).toBeGreaterThanOrEqual(7);
      for (const b of buttons) {
        expect(b.textContent ?? '', b.outerHTML).not.toMatch(FORBIDDEN);
        expect(b.getAttribute('title') ?? '').not.toMatch(FORBIDDEN);
      }
    });
  }

  it('disables the write buttons while a write runs', () => {
    const t = makeT('en');
    render(<Toolbar t={t} actions={actions()} mode="list" busy editor="code" theme="auto" showEditor />);
    expect((screen.getByText(t('toolbar.align')).closest('button') as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByText(t('toolbar.createFolder')).closest('button') as HTMLButtonElement).disabled).toBe(true);
  });
});
