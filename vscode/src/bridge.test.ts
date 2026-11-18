import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const h = vi.hoisted(() => ({
  executeCommand: vi.fn(async (..._a: unknown[]) => undefined),
  writeText: vi.fn(async (_t: string) => undefined),
  showErrorMessage: vi.fn(async (_m: string) => undefined),
}));

vi.mock('vscode', () => ({
  commands: { executeCommand: h.executeCommand },
  env: { clipboard: { writeText: h.writeText } },
  window: { showErrorMessage: h.showErrorMessage },
  l10n: { t: (s: string, ...a: unknown[]) => s.replace(/\{(\d+)\}/g, (_m, i) => String(a[Number(i)])) },
  Uri: {
    file: (p: string) => ({ scheme: 'file', fsPath: p }),
    from: (c: object) => ({ ...c }),
  },
}));

import { Generation, handlePageMessage, handleWebviewMessage, unsafeFolderPath, type BridgeDeps } from './bridge';

let posted: any[];
let call: ReturnType<typeof vi.fn>;
let openWide: ReturnType<typeof vi.fn>;
let deps: BridgeDeps;

beforeEach(() => {
  posted = [];
  call = vi.fn(async () => ({ ok: true }));
  openWide = vi.fn();
  h.executeCommand.mockClear();
  h.writeText.mockClear();
  h.showErrorMessage.mockClear();
  deps = { core: { call } as any, post: (m) => posted.push(m), openWide: openWide as any, log: () => {} };
});

describe('handleWebviewMessage', () => {
  it('forwards call and answers by id', async () => {
    await handleWebviewMessage({ type: 'call', id: 4, method: 'view', params: { repo: 'r' } }, deps);
    expect(call).toHaveBeenCalledWith('view', { repo: 'r' });
    expect(posted).toEqual([{ type: 'result', id: 4, result: { ok: true } }]);
  });

  it('answers a failed call with the error', async () => {
    call.mockRejectedValueOnce(Object.assign(new Error('boom'), { code: 'x' }));
    await handleWebviewMessage({ type: 'call', id: 5, method: 'view' }, deps);
    expect(posted).toEqual([{ type: 'result', id: 5, error: { code: 'x', message: 'boom' } }]);
  });

  it('ignores unknown message types and junk', async () => {
    for (const m of [{ type: 'exec', cmd: 'rm' }, { type: 5 }, null, 'x', 42, { id: 1 }]) {
      await handleWebviewMessage(m, deps);
    }
    expect(call).not.toHaveBeenCalled();
    expect(h.executeCommand).not.toHaveBeenCalled();
    expect(h.writeText).not.toHaveBeenCalled();
    expect(posted).toEqual([]);
  });

  it('rejects a call with a bad method name or params', async () => {
    await handleWebviewMessage({ type: 'call', id: 2, method: 'view; rm' }, deps);
    await handleWebviewMessage({ type: 'call', id: 3, method: 'view', params: [1] }, deps);
    expect(call).not.toHaveBeenCalled();
    expect(posted.map((p) => p.id)).toEqual([2, 3]);
    expect(posted.every((p) => p.error)).toBe(true);
  });

  it('ignores a call without a numeric id', async () => {
    await handleWebviewMessage({ type: 'call', id: 'x', method: 'view' }, deps);
    expect(call).not.toHaveBeenCalled();
    expect(posted).toEqual([]);
  });

  it('refuses openFolder for relative and missing paths', async () => {
    await handleWebviewMessage({ type: 'openFolder', path: 'relative/dir' }, deps);
    await handleWebviewMessage({ type: 'openFolder', path: path.join(os.tmpdir(), 'gg-does-not-exist-xyz') }, deps);
    expect(h.executeCommand).not.toHaveBeenCalled();
    expect(h.showErrorMessage).toHaveBeenCalledTimes(2);
  });

  it('refuses openFolder for a file', async () => {
    const f = path.join(fs.mkdtempSync(path.join(os.tmpdir(), 'gg-')), 'a.txt');
    fs.writeFileSync(f, 'x');
    await handleWebviewMessage({ type: 'openFolder', path: f }, deps);
    expect(h.executeCommand).not.toHaveBeenCalled();
  });

  it('opens an existing absolute folder in a new window', async () => {
    const d = fs.mkdtempSync(path.join(os.tmpdir(), 'gg-'));
    await handleWebviewMessage({ type: 'openFolder', path: d }, deps);
    expect(h.executeCommand).toHaveBeenCalledWith('vscode.openFolder', { scheme: 'file', fsPath: d }, { forceNewWindow: true });
  });

  it('copies text and opens the wide tab', async () => {
    await handleWebviewMessage({ type: 'copy', text: 'abc' }, deps);
    await handleWebviewMessage({ type: 'openWide' }, deps);
    expect(h.writeText).toHaveBeenCalledWith('abc');
    expect(openWide).toHaveBeenCalled();
  });

  it('opens a diff between two gitglance-git documents and answers true', async () => {
    await handleWebviewMessage({ type: 'openDiff', id: 9, repo: '/r', a: 'main', b: 'v2', path: 'src/a.ts' }, deps);
    const [cmd, left, right] = h.executeCommand.mock.calls[0] as any[];
    expect(cmd).toBe('vscode.diff');
    expect(left.scheme).toBe('gitglance-git');
    expect(right.scheme).toBe('gitglance-git');
    expect(new URLSearchParams(left.query).get('ref')).toBe('main');
    expect(new URLSearchParams(right.query).get('path')).toBe('src/a.ts');
    expect(posted).toEqual([{ type: 'result', id: 9, result: true }]);
  });

  it('rejects openDiff with missing fields', async () => {
    await handleWebviewMessage({ type: 'openDiff', id: 9, repo: '/r', a: 'main' }, deps);
    expect(h.executeCommand).not.toHaveBeenCalled();
    expect(posted[0]).toMatchObject({ type: 'result', id: 9, error: { code: 'bad-message' } });
  });
});

describe('openFolder path safety', () => {
  it('flags UNC and root-relative paths', () => {
    expect(unsafeFolderPath(String.raw`\\host\share`, 'win32')).toBe(true);
    expect(unsafeFolderPath('//host/share', 'linux')).toBe(true);
    expect(unsafeFolderPath(String.raw`\foo`, 'win32')).toBe(true);
    expect(unsafeFolderPath('/foo', 'win32')).toBe(true);
    expect(unsafeFolderPath('/home/a', 'linux')).toBe(false);
    expect(unsafeFolderPath(String.raw`C:\work\repo`, 'win32')).toBe(false);
  });

  it('refuses a UNC path without touching the file system', async () => {
    await handleWebviewMessage({ type: 'openFolder', path: String.raw`\\server\share\x` }, deps);
    await handleWebviewMessage({ type: 'openFolder', path: '//server/share/x' }, deps);
    expect(h.executeCommand).not.toHaveBeenCalled();
    expect(h.showErrorMessage).toHaveBeenCalledTimes(2);
  });
});

describe('page tokens', () => {
  it('gives every render a new, unguessable token', () => {
    const gen = new Generation();
    const a = gen.next();
    const b = gen.next();
    expect(a).toMatch(/^[0-9a-f]{32}$/);
    expect(b).not.toBe(a);
  });

  it('drops a reply for a render that was replaced while the call was pending', async () => {
    let finish!: (v: unknown) => void;
    call.mockImplementationOnce(() => new Promise((r) => (finish = r)));
    const gen = new Generation();
    const old = gen.next();
    const pending = handlePageMessage({ type: 'call', id: 1, method: 'view', page: old }, gen, deps);
    const fresh = gen.next(); // the page was reloaded
    finish({ old: true });
    await pending;
    expect(posted).toEqual([]);
    // a call made after the reload is answered
    await handlePageMessage({ type: 'call', id: 1, method: 'view', page: fresh }, gen, deps);
    expect(posted).toEqual([{ type: 'result', id: 1, result: { ok: true } }]);
  });

  it('ignores a message of an older render that arrives after the reload', async () => {
    const gen = new Generation();
    const old = gen.next();
    gen.next();
    await handlePageMessage({ type: 'call', id: 1, method: 'view', page: old }, gen, deps);
    await handlePageMessage({ type: 'copy', text: 'x', page: old }, gen, deps);
    await handlePageMessage({ type: 'openWide', page: old }, gen, deps);
    expect(call).not.toHaveBeenCalled();
    expect(h.writeText).not.toHaveBeenCalled();
    expect(openWide).not.toHaveBeenCalled();
    expect(posted).toEqual([]);
  });

  it('ignores a message without a token', async () => {
    const gen = new Generation();
    gen.next();
    await handlePageMessage({ type: 'call', id: 1, method: 'view' }, gen, deps);
    await handlePageMessage({ type: 'call', id: 2, method: 'view', page: 7 }, gen, deps);
    expect(call).not.toHaveBeenCalled();
    expect(posted).toEqual([]);
  });

  it('serves a message of the current render', async () => {
    const gen = new Generation();
    const page = gen.next();
    await handlePageMessage({ type: 'call', id: 3, method: 'repos', page }, gen, deps);
    expect(call).toHaveBeenCalledWith('repos', undefined);
    expect(posted).toEqual([{ type: 'result', id: 3, result: { ok: true } }]);
  });
});

describe('repository list during startup discovery', () => {
  it('asks the core for repos only after beforeRepos settles', async () => {
    let open!: () => void;
    const beforeRepos = vi.fn(() => new Promise<void>((r) => (open = r)));
    const pending = handleWebviewMessage({ type: 'call', id: 1, method: 'repos' }, { ...deps, beforeRepos });
    await Promise.resolve();
    expect(beforeRepos).toHaveBeenCalledTimes(1);
    expect(call).not.toHaveBeenCalled();
    open();
    await pending;
    expect(call).toHaveBeenCalledWith('repos', undefined);
    expect(posted).toEqual([{ type: 'result', id: 1, result: { ok: true } }]);
  });

  it('does not hold other methods', async () => {
    const beforeRepos = vi.fn(() => new Promise<void>(() => undefined));
    await handleWebviewMessage({ type: 'call', id: 1, method: 'view', params: { repo: '/r' } }, { ...deps, beforeRepos });
    expect(beforeRepos).not.toHaveBeenCalled();
    expect(call).toHaveBeenCalledWith('view', { repo: '/r' });
  });
});
