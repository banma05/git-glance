import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { vscodeHost } from './vscode';

let sent: any[];

beforeEach(() => {
  sent = [];
  (window as any).acquireVsCodeApi = () => ({ postMessage: (m: unknown) => sent.push(m) });
  (window as any).__GG_INIT__ = { mode: 'wide', locale: 'zh-cn', theme: 'light' };
});
afterEach(() => {
  delete (window as any).acquireVsCodeApi;
  delete (window as any).__GG_INIT__;
});

const deliver = (data: unknown) => window.dispatchEvent(new MessageEvent('message', { data }));

describe('vscodeHost', () => {
  it('reads mode, locale and theme from the init object', () => {
    const h = vscodeHost();
    expect([h.mode, h.locale, h.theme()]).toEqual(['wide', 'zh-cn', 'light']);
  });

  it('falls back to the gg-init meta tag when no init object is set', () => {
    delete (window as any).__GG_INIT__;
    const meta = document.createElement('meta');
    meta.name = 'gg-init';
    meta.content = JSON.stringify({ mode: 'wide', locale: 'zh-cn', theme: 'light' });
    document.head.appendChild(meta);
    try {
      const h = vscodeHost();
      expect([h.mode, h.locale, h.theme()]).toEqual(['wide', 'zh-cn', 'light']);
    } finally {
      meta.remove();
    }
  });

  it('matches results to calls by id', async () => {
    const h = vscodeHost();
    const p = h.call<number>('version', { a: 1 });
    expect(sent[0]).toMatchObject({ type: 'call', method: 'version', params: { a: 1 } });
    deliver({ type: 'result', id: sent[0].id, result: 7 });
    expect(await p).toBe(7);
  });

  it('rejects a call with the code and message of an error', async () => {
    const h = vscodeHost();
    const p = h.call('view');
    deliver({ type: 'result', id: sent[0].id, error: { code: 'git-failed', message: 'boom' } });
    await expect(p).rejects.toMatchObject({ code: 'git-failed', message: 'boom' });
  });

  it('posts openFolder, copy, openDiff and openWide', async () => {
    const h = vscodeHost();
    void h.openFolder('/p');
    void h.copy('txt');
    h.openWide();
    const d = h.openDiff('/r', 'a', 'b', 'f');
    expect(sent.map((m) => m.type)).toEqual(['openFolder', 'copy', 'openWide', 'openDiff']);
    expect(sent[3]).toMatchObject({ repo: '/r', a: 'a', b: 'b', path: 'f' });
    deliver({ type: 'result', id: sent[3].id, result: true });
    expect(await d).toBe(true);
  });

  it('tags every message with the page token of its render', async () => {
    (window as any).__GG_INIT__ = { mode: 'sidebar', locale: 'en', theme: 'dark', page: 'tok-1' };
    const h = vscodeHost();
    void h.call('repos');
    void h.openFolder('/p');
    void h.copy('txt');
    h.openWide();
    void h.openDiff('/r', 'a', 'b', 'f');
    expect(sent.map((m) => m.page)).toEqual(['tok-1', 'tok-1', 'tok-1', 'tok-1', 'tok-1']);
  });

  it('tags every message with one instance id made when the page loads', () => {
    const a = vscodeHost();
    void a.call('repos');
    a.openWide();
    const first = sent.map((m) => m.instance);
    expect(first[0]).toMatch(/^[0-9a-f]{32}$/);
    expect(first[1]).toBe(first[0]);
    sent.length = 0;
    vscodeHost(); // a page loaded again
    const b = vscodeHost();
    void b.call('repos');
    expect(sent[0].instance).not.toBe(first[0]);
  });

  it('passes on the problems of workspace discovery', () => {
    const h = vscodeHost();
    const cb = vi.fn();
    h.onDiscovery?.(cb);
    deliver({ type: 'discovery', problems: [{ folder: '/w', code: 'core-unavailable', message: 'x' }, { junk: 1 }] });
    expect(cb).toHaveBeenCalledWith([{ folder: '/w', code: 'core-unavailable', message: 'x' }]);
  });

  it('tells the page to read again when the extension says refresh', () => {
    const h = vscodeHost();
    const cb = vi.fn();
    h.onRefresh?.(cb);
    deliver({ type: 'refresh' });
    expect(cb).toHaveBeenCalledTimes(1);
  });

  it('notifies theme changes', () => {
    const h = vscodeHost();
    const cb = vi.fn();
    h.onTheme(cb);
    deliver({ type: 'theme', kind: 'dark' });
    expect(cb).toHaveBeenCalledWith('dark');
    expect(h.theme()).toBe('dark');
  });
});
