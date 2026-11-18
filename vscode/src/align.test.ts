import { beforeEach, describe, expect, it, vi } from 'vitest';

const h = vi.hoisted(() => ({
  info: vi.fn(async (..._a: unknown[]): Promise<unknown> => undefined),
  warn: vi.fn(async (..._a: unknown[]): Promise<unknown> => undefined),
  error: vi.fn(async (..._a: unknown[]): Promise<unknown> => undefined),
}));

vi.mock('vscode', () => ({
  window: { showInformationMessage: h.info, showWarningMessage: h.warn, showErrorMessage: h.error },
  l10n: { t: (s: string, ...a: unknown[]) => s.replace(/\{(\d+)\}/g, (_m, i) => String(a[Number(i)])) },
}));

import { alignAll, alignSummary } from './align';

const report = (updated: number, skipped: number, fetchOk = true) => ({
  fetch: { ok: fetchOk, error: fetchOk ? '' : 'could not resolve host' },
  updated: Array(updated).fill({}),
  skipped: Array(skipped).fill({}),
});

beforeEach(() => {
  h.info.mockReset();
  h.warn.mockReset();
  h.error.mockReset();
});

describe('alignSummary', () => {
  it('counts the updated and skipped branches', () => {
    const s = alignSummary([
      { repo: '/w/a', report: report(2, 1), error: '' },
      { repo: '/w/b', report: report(1, 0), error: '' },
    ]);
    expect(s).toEqual({ text: 'Git Glance: 3 branches updated, 1 left alone.', problems: false });
  });

  it('lists the repositories that failed and those that could not read the remote', () => {
    const s = alignSummary([
      { repo: '/w/a', report: report(1, 0), error: '' },
      { repo: 'C:\\w\\b', report: null, error: 'the core program stopped' },
      { repo: '/w/c', report: report(0, 2, false), error: '' },
    ]);
    expect(s.problems).toBe(true);
    expect(s.text).toContain('1 branches updated, 2 left alone.');
    expect(s.text).toContain('Not aligned: b (the core program stopped).');
    expect(s.text).toContain('Could not read the remote of c (could not resolve host)');
  });
});

describe('alignAll', () => {
  it('reads the repositories only once workspace discovery is done', async () => {
    let discovered!: () => void;
    const call = vi.fn(async (method: string) => (method === 'repos' ? [] : report(0, 0)));
    h.info.mockResolvedValueOnce('Align');
    const run = alignAll({ call } as any, () => undefined, () => undefined, () => new Promise<void>((r) => (discovered = r)));
    await new Promise((r) => setTimeout(r, 10));
    expect(call).not.toHaveBeenCalled();
    discovered();
    await run;
    expect(call).toHaveBeenCalledWith('repos');
  });

  it('does nothing without the confirm', async () => {
    const call = vi.fn();
    const done = vi.fn();
    h.info.mockResolvedValueOnce(undefined);
    await alignAll({ call } as any, () => undefined, done);
    expect(call).not.toHaveBeenCalled();
    expect(done).not.toHaveBeenCalled();
  });

  it('aligns every repository, keeps going after a failure, refreshes the pages and warns', async () => {
    const call = vi.fn(async (method: string, params?: any) => {
      if (method === 'repos') return ['/w/a', '/w/b'];
      if (params.repo === '/w/a') throw new Error('timeout');
      return report(1, 0);
    });
    const done = vi.fn();
    h.info.mockResolvedValueOnce('Align');
    await alignAll({ call } as any, () => undefined, done);
    expect(call.mock.calls.map((c) => [c[0], c[1]?.repo])).toEqual([
      ['repos', undefined],
      ['align', '/w/a'],
      ['align', '/w/b'],
    ]);
    expect(done).toHaveBeenCalledTimes(1);
    expect(h.warn).toHaveBeenCalledTimes(1);
    expect(String(h.warn.mock.calls[0][0])).toContain('Not aligned: a (timeout).');
  });
});
