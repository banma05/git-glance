import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/preact';
import { TrunkNews } from './TrunkNews';
import type { NewsSource } from './TrunkNews';
import { dateTime } from '../format';
import { makeT } from '../i18n';
import { HostError } from '../host/types';
import type { Host } from '../host/types';
import type { CommitFiles, CommitInfo, TrunkNews as News } from '../types';

const t = makeT('en');
const hex = (c: string) => c.repeat(40).slice(0, 40);
const commit = (c: string, subject: string, time: number, author: string): CommitInfo => ({ hash: hex(c), subject, time, author });
const COMMITS = [
  commit('c', 'Merge feature/search', 1_700_000_300, 'carol'),
  commit('b', 'Fix login redirect', 1_700_000_200, 'bob'),
  commit('a', 'Add a footer', 1_700_000_100, 'alice'),
];
const NEWS: News = {
  commits: COMMITS,
  total: 3,
  base: hex('0'),
  tip: hex('c'),
  files: [
    { path: 'src/search.ts', status: 'A' },
    { path: 'src/login.ts', status: 'M' },
  ],
  filesTruncated: false,
};
const FILES: CommitFiles = { parent: hex('9'), files: [{ path: 'src/login.ts', status: 'M' }, { path: 'old.txt', status: 'D' }], truncated: false };
const READS = ['trunkNews', 'recentCommits', 'commitFiles', 'fileDiff', 'fileAt'];

function fakeHost(over: Record<string, unknown> = {}) {
  const calls: Array<{ method: string; params: Record<string, unknown> }> = [];
  const openDiff = vi.fn(async (..._args: unknown[]) => false);
  const host = {
    async call(method: string, params: Record<string, unknown> = {}) {
      calls.push({ method, params });
      const v = over[method];
      if (v instanceof Error) throw v;
      if (v !== undefined) return typeof v === 'function' ? (v as () => unknown)() : v;
      switch (method) {
        case 'trunkNews':
          return NEWS;
        case 'recentCommits':
          return COMMITS;
        case 'commitFiles':
          return FILES;
        case 'fileDiff':
          return { diff: '@@ -1 +1 @@\n-old line\n+new line', truncated: false };
        case 'fileAt':
          return { text: 'first text', binary: false, truncated: false };
        default:
          throw new HostError('unknown-method', method);
      }
    },
    openDiff: (repo: string, a: string, b: string, path: string) => openDiff(repo, a, b, path),
  } as unknown as Host;
  const count = (m: string) => calls.filter((c) => c.method === m).length;
  return { host, calls, openDiff, count };
}

const NEWS_SOURCE: NewsSource = {
  kind: 'news',
  trunk: 'refs/remotes/origin/main',
  branch: 'refs/heads/feature/login',
  trunkName: 'main',
  branchName: 'feature/login',
};

function show(h = fakeHost(), source: NewsSource = NEWS_SOURCE, onClose = vi.fn()) {
  const utils = render(<TrunkNews t={t} host={h.host} repo="/w/repo" source={source} fetchedAt={1_700_000_400} onClose={onClose} />);
  return { ...utils, h, onClose };
}
const rows = () => screen.getAllByRole('button').filter((b) => b.classList.contains('news-commit'));

afterEach(cleanup);

describe('TrunkNews', () => {
  it('lists the trunk commits the branch lacks, newest first, with hash, subject, time and author', async () => {
    const { h } = show();
    await screen.findByText('Merge feature/search');
    expect(h.calls[0]).toEqual({ method: 'trunkNews', params: { repo: '/w/repo', trunk: 'refs/remotes/origin/main', branch: 'refs/heads/feature/login' } });
    const list = rows();
    expect(list.map((r) => r.querySelector('code')!.textContent)).toEqual(['ccccccc', 'bbbbbbb', 'aaaaaaa']);
    expect(list[0].textContent).toContain('Merge feature/search');
    expect(list[0].textContent).toContain('carol');
    expect(list[0].textContent).toContain(dateTime(1_700_000_300));
    expect(screen.getByText('New on main, not yet in feature/login')).toBeTruthy();
    expect(screen.getByText(`As of the last read of the remote (${dateTime(1_700_000_400)})`)).toBeTruthy();
    expect(screen.getByText('3 commits')).toBeTruthy();
  });

  it('gives the true total and the shown count when the list is cut', async () => {
    show(fakeHost({ trunkNews: { ...NEWS, total: 205 } }));
    expect(await screen.findByText('205 commits; the newest 3 are listed')).toBeTruthy();
  });

  it('says the branch has every trunk commit when there is none', async () => {
    show(fakeHost({ trunkNews: { ...NEWS, commits: [], total: 0, files: [], base: hex('c') } }));
    expect(await screen.findByText('The branch has every trunk commit')).toBeTruthy();
    expect(rows()).toHaveLength(0);
  });

  it('opens a commit once: its files, then a diff from its first parent', async () => {
    const { h } = show();
    await screen.findByText('Fix login redirect');
    const row = rows()[1];
    expect(row.getAttribute('aria-expanded')).toBe('false');
    fireEvent.click(row);
    await screen.findByText('old.txt');
    expect(row.getAttribute('aria-expanded')).toBe('true');
    expect(h.calls.find((c) => c.method === 'commitFiles')?.params).toEqual({ repo: '/w/repo', hash: hex('b') });
    fireEvent.click(row); // closes
    expect(screen.queryByText('old.txt')).toBeNull();
    fireEvent.click(row); // opens again from what was read
    await screen.findByText('old.txt');
    expect(h.count('commitFiles')).toBe(1);

    fireEvent.click(screen.getByText('src/login.ts'));
    await waitFor(() => expect(h.openDiff).toHaveBeenCalledWith('/w/repo', hex('9'), hex('b'), 'src/login.ts'));
    await waitFor(() => expect(h.calls.find((c) => c.method === 'fileDiff')?.params).toEqual({ repo: '/w/repo', a: hex('9'), b: hex('b'), path: 'src/login.ts' }));
    expect(await screen.findByText(/\+new line/)).toBeTruthy();
  });

  it('does not show the diff in the page when the editor opened it', async () => {
    const h = fakeHost();
    h.openDiff.mockResolvedValue(true);
    show(h);
    fireEvent.click((await screen.findAllByText('Fix login redirect'))[0]);
    fireEvent.click(await screen.findByText('src/login.ts'));
    await waitFor(() => expect(h.openDiff).toHaveBeenCalledTimes(1));
    expect(h.count('fileDiff')).toBe(0);
  });

  it('shows a file of a root commit as the added text, without a diff', async () => {
    const h = fakeHost({ commitFiles: { parent: '', files: [{ path: 'README.md', status: 'A' }], truncated: false } });
    show(h);
    fireEvent.click((await screen.findAllByText('Add a footer'))[0]);
    fireEvent.click(await screen.findByText('README.md'));
    expect(await screen.findByText('first text')).toBeTruthy();
    expect(h.calls.find((c) => c.method === 'fileAt')?.params).toEqual({ repo: '/w/repo', ref: hex('a'), path: 'README.md' });
    expect(h.openDiff).not.toHaveBeenCalled();
    expect(h.count('fileDiff')).toBe(0);
  });

  it('shows the files combined, from the fork point to the trunk tip, with their diffs', async () => {
    const { h } = show();
    await screen.findByText('Merge feature/search');
    fireEvent.click(screen.getByRole('button', { name: 'Combined' }));
    expect(screen.getByRole('button', { name: 'Combined' }).getAttribute('aria-pressed')).toBe('true');
    expect(rows()).toHaveLength(0);
    expect(screen.getByText('src/search.ts')).toBeTruthy();
    fireEvent.click(screen.getByText('src/login.ts'));
    await waitFor(() => expect(h.openDiff).toHaveBeenCalledWith('/w/repo', hex('0'), hex('c'), 'src/login.ts'));
    fireEvent.click(screen.getByRole('button', { name: 'Commit by commit' }));
    expect(rows()).toHaveLength(3);
  });

  it('says the combined view is not available when the branch and the trunk share no commit', async () => {
    show(fakeHost({ trunkNews: { ...NEWS, base: '', files: [] } }));
    await screen.findByText('Merge feature/search');
    fireEvent.click(screen.getByRole('button', { name: 'Combined' }));
    expect(screen.getByText('Not available: the branch and the trunk share no commit')).toBeTruthy();
  });

  it('reads the list again on Refresh and sends nothing but reads', async () => {
    const { h } = show();
    await screen.findByText('Merge feature/search');
    fireEvent.click(rows()[0]);
    await screen.findByText('old.txt');
    fireEvent.click(screen.getByText('src/login.ts'));
    await waitFor(() => expect(h.count('fileDiff')).toBe(1));
    fireEvent.click(screen.getByRole('button', { name: 'Refresh list' }));
    await waitFor(() => expect(h.count('trunkNews')).toBe(2));
    expect(h.calls.every((c) => READS.includes(c.method))).toBe(true);
  });

  it('lists the recent commits of a trunk, with no combined view', async () => {
    const { h } = show(fakeHost(), { kind: 'recent', ref: 'refs/remotes/origin/main', trunkName: 'main' });
    await screen.findByText('Merge feature/search');
    expect(h.calls[0]).toEqual({ method: 'recentCommits', params: { repo: '/w/repo', ref: 'refs/remotes/origin/main', limit: 50 } });
    expect(screen.getByText('Recent commits on main')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Combined' })).toBeNull();
    expect(rows()).toHaveLength(3);
  });

  it('shows a failed read with Retry, and closes with Close', async () => {
    const { h, onClose } = show(fakeHost({ trunkNews: new HostError('git-failed', 'exit 128') }));
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('A git command failed');
    fireEvent.click(within(alert).getByRole('button', { name: 'Retry' }));
    await waitFor(() => expect(h.count('trunkNews')).toBe(2));
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(onClose).toHaveBeenCalled();
  });
});
