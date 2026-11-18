import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Discovery } from './startup';
import type { DiscoveryOutcome } from './startup';

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
});
afterEach(() => {
  vi.useRealTimers();
});

const deferred = () => {
  let resolve!: (v: DiscoveryOutcome) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<DiscoveryOutcome>((a, b) => {
    resolve = a;
    reject = b;
  });
  return { promise, resolve, reject };
};
const fine: DiscoveryOutcome = { repos: 1, problems: [] };

function started(limit = 15_000) {
  const lines: string[] = [];
  const d = new Discovery(limit, (l) => lines.push(l));
  const work = deferred();
  d.run(() => work.promise);
  return { d, work, lines };
}

describe('Discovery', () => {
  it('holds the repository list until startup discovery has finished', async () => {
    const { d, work } = started();
    let passed = false;
    const notify = vi.fn();
    void d.beforeRepos('page', notify).then(() => (passed = true));
    await vi.advanceTimersByTimeAsync(5_000);
    expect(passed).toBe(false);
    work.resolve(fine);
    await vi.advanceTimersByTimeAsync(0);
    expect(passed).toBe(true);
    expect(notify).not.toHaveBeenCalled(); // the page was answered after discovery
  });

  it('answers after the limit, logs that, and tells the page to read again when discovery ends', async () => {
    const { d, work, lines } = started();
    let passed = false;
    const notify = vi.fn();
    void d.beforeRepos('page', notify).then(() => (passed = true));
    await vi.advanceTimersByTimeAsync(15_001);
    expect(passed).toBe(true);
    expect(lines.join('\n')).toMatch(/held the repository list for 15000 ms/);
    expect(notify).not.toHaveBeenCalled();
    work.resolve(fine);
    await vi.advanceTimersByTimeAsync(0);
    expect(notify).toHaveBeenCalledTimes(1);
  });

  it('tells each early page once, also when discovery failed', async () => {
    const { d, work } = started(10);
    const a = vi.fn();
    const b = vi.fn();
    void d.beforeRepos('a', a);
    void d.beforeRepos('a', a);
    void d.beforeRepos('b', b);
    await vi.advanceTimersByTimeAsync(20);
    work.reject(new Error('core-unavailable'));
    await vi.advanceTimersByTimeAsync(0);
    expect(a).toHaveBeenCalledTimes(1);
    expect(b).toHaveBeenCalledTimes(1);
  });

  it('does not wait once discovery has finished', async () => {
    const { d, work } = started();
    work.resolve(fine);
    await vi.advanceTimersByTimeAsync(0);
    const notify = vi.fn();
    let passed = false;
    void d.beforeRepos('page', notify).then(() => (passed = true));
    await vi.advanceTimersByTimeAsync(0);
    expect(passed).toBe(true);
    expect(notify).not.toHaveBeenCalled();
  });

  it('leaves no timer behind', async () => {
    const { d, work } = started();
    void d.beforeRepos('page', () => undefined);
    work.resolve(fine);
    await vi.advanceTimersByTimeAsync(0);
    expect(vi.getTimerCount()).toBe(0);
  });

  it('logs how discovery ended: repositories, skipped folders, time', async () => {
    const { work, lines } = started();
    work.resolve({ repos: 2, problems: [{ folder: '/w/x', code: 'not-repo', message: 'no' }] });
    await vi.advanceTimersByTimeAsync(0);
    expect(lines.join('\n')).toMatch(/discovery done: 2 repositories listed, 1 folders skipped, \d+ ms/);
  });

  it('hands the problems of the finished run to the page', async () => {
    const { d, work } = started();
    const problems = [{ folder: '/w/x', code: 'core-unavailable', message: 'could not start' }];
    work.resolve({ repos: 0, problems });
    expect(await d.beforeRepos('page', () => undefined)).toEqual(problems);
  });

  it('turns a failed run into a problem the page can show', async () => {
    const { d, work } = started();
    work.reject(Object.assign(new Error('boom'), { code: 'internal' }));
    expect(await d.beforeRepos('page', () => undefined)).toEqual([{ folder: '', code: 'internal', message: 'boom' }]);
  });

  it('runs again on demand: pages wait for the new run and get its problems', async () => {
    const { d, work } = started();
    work.resolve({ repos: 0, problems: [{ folder: '/w', code: 'core-unavailable', message: 'x' }] });
    await vi.advanceTimersByTimeAsync(0);
    const again = deferred();
    d.run(() => again.promise);
    expect(d.done).toBe(false);
    let got: unknown;
    void d.beforeRepos('page', () => undefined).then((p) => (got = p));
    await vi.advanceTimersByTimeAsync(1000);
    expect(got).toBeUndefined();
    again.resolve({ repos: 1, problems: [] });
    await vi.advanceTimersByTimeAsync(0);
    expect(got).toEqual([]);
  });

  it('lets only the latest run finish it', async () => {
    const { d, work } = started();
    const again = deferred();
    d.run(() => again.promise);
    work.resolve({ repos: 9, problems: [{ folder: '/old', code: 'x', message: 'x' }] });
    await vi.advanceTimersByTimeAsync(0);
    expect(d.done).toBe(false);
    again.resolve(fine);
    await vi.advanceTimersByTimeAsync(0);
    expect(d.done).toBe(true);
    expect(await d.beforeRepos('page', () => undefined)).toEqual([]);
  });
});
