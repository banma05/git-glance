import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Discovery } from './startup';

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
});
afterEach(() => {
  vi.useRealTimers();
});

const deferred = () => {
  let resolve!: () => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<void>((a, b) => {
    resolve = a;
    reject = b;
  });
  return { promise, resolve, reject };
};

describe('Discovery', () => {
  it('holds the repository list until startup discovery has finished', async () => {
    const work = deferred();
    const d = new Discovery(work.promise, 15_000);
    let passed = false;
    const notify = vi.fn();
    void d.beforeRepos('page', notify).then(() => (passed = true));
    await vi.advanceTimersByTimeAsync(5_000);
    expect(passed).toBe(false);
    work.resolve();
    await vi.advanceTimersByTimeAsync(0);
    expect(passed).toBe(true);
    expect(notify).not.toHaveBeenCalled(); // the page was answered after discovery
  });

  it('answers after the limit and tells the page to read again when discovery ends', async () => {
    const work = deferred();
    const d = new Discovery(work.promise, 15_000);
    let passed = false;
    const notify = vi.fn();
    void d.beforeRepos('page', notify).then(() => (passed = true));
    await vi.advanceTimersByTimeAsync(15_001);
    expect(passed).toBe(true);
    expect(notify).not.toHaveBeenCalled();
    work.resolve();
    await vi.advanceTimersByTimeAsync(0);
    expect(notify).toHaveBeenCalledTimes(1);
  });

  it('tells each early page once, also when discovery failed', async () => {
    const work = deferred();
    const d = new Discovery(work.promise, 10);
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
    const work = deferred();
    const d = new Discovery(work.promise, 15_000);
    work.resolve();
    await vi.advanceTimersByTimeAsync(0);
    const notify = vi.fn();
    let passed = false;
    void d.beforeRepos('page', notify).then(() => (passed = true));
    await vi.advanceTimersByTimeAsync(0);
    expect(passed).toBe(true);
    expect(notify).not.toHaveBeenCalled();
  });

  it('leaves no timer behind', async () => {
    const work = deferred();
    const d = new Discovery(work.promise, 15_000);
    void d.beforeRepos('page', () => undefined);
    work.resolve();
    await vi.advanceTimersByTimeAsync(0);
    expect(vi.getTimerCount()).toBe(0);
  });
});
