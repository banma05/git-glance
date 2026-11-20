import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const h = vi.hoisted(() => ({
  executeCommand: vi.fn(async (..._a: unknown[]) => undefined),
  writeText: vi.fn(async (_t: string) => undefined),
  showErrorMessage: vi.fn(async (_m: string) => undefined),
  docs: [] as Array<{ uri: { scheme: string; fsPath: string }; isDirty: boolean }>,
  notebooks: [] as Array<{ uri: { scheme: string; fsPath: string }; isDirty: boolean }>,
  tabs: [] as Array<{ isDirty: boolean; input: unknown }>,
}));

vi.mock('vscode', () => ({
  commands: { executeCommand: h.executeCommand },
  env: { clipboard: { writeText: h.writeText } },
  window: {
    showErrorMessage: h.showErrorMessage,
    tabGroups: {
      get all() {
        return [{ tabs: h.tabs }];
      },
    },
  },
  workspace: {
    get textDocuments() {
      return h.docs;
    },
    get notebookDocuments() {
      return h.notebooks;
    },
  },
  l10n: { t: (s: string, ...a: unknown[]) => s.replace(/\{(\d+)\}/g, (_m, i) => String(a[Number(i)])) },
  Uri: {
    file: (p: string) => ({ scheme: 'file', fsPath: p }),
    from: (c: object) => ({ ...c }),
  },
}));

import { Generation, handlePageMessage, handleWebviewMessage, unsafeFolderPath, unsavedUnder, type BridgeDeps } from './bridge';

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
  h.docs = [];
  h.notebooks = [];
  h.tabs = [];
  deps = { core: { call } as any, post: (m) => posted.push(m), openWide: openWide as any, log: () => {} };
});

describe('switch under unsaved editors', () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'gg-main-'));
  const doc = (p: string, isDirty: boolean, scheme = 'file') => ({ uri: { scheme, fsPath: p }, isDirty });
  const params = { repo: root, folder: root, branch: 'main', latest: true, shelve: true };

  it('refuses switch while editors are dirty', async () => {
    h.docs = [doc(path.join(root, 'src', 'login.ts'), true), doc(path.join(root, 'README.md'), false)];
    await handleWebviewMessage({ type: 'call', id: 7, method: 'switch', params }, deps);
    expect(call).not.toHaveBeenCalled();
    expect(posted).toEqual([
      {
        type: 'result',
        id: 7,
        result: {
          ok: false,
          reason: 'unsaved-editors',
          files: ['src/login.ts'],
          path: root,
          detail: '',
          logError: '',
          shelved: null,
          restored: null,
          keptShelf: null,
          fastForwarded: false,
          noFastForward: '',
          aheadNote: '',
        },
      },
    ]);
  });

  it('forwards switch when only other folders have unsaved editors', async () => {
    h.docs = [
      doc(root + '-login' + path.sep + 'a.ts', true), // a sibling folder whose name starts the same
      doc(path.join(os.tmpdir(), 'elsewhere.ts'), true),
      doc('Untitled-1', true, 'untitled'),
      doc(path.join(root, 'saved.ts'), false),
    ];
    await handleWebviewMessage({ type: 'call', id: 8, method: 'switch', params }, deps);
    expect(call).toHaveBeenCalledWith('switch', params, undefined, undefined);
    expect(posted).toEqual([{ type: 'result', id: 8, result: { ok: true } }]);
  });

  it('refuses switch while a notebook under the folder is dirty', async () => {
    h.notebooks = [doc(path.join(root, 'nb', 'study.ipynb'), true)];
    await handleWebviewMessage({ type: 'call', id: 10, method: 'switch', params }, deps);
    expect(call).not.toHaveBeenCalled();
    expect(posted[0].result).toMatchObject({ ok: false, reason: 'unsaved-editors', files: ['nb/study.ipynb'] });
  });

  it('sees a file that is under the folder through a link or junction', async () => {
    const real = fs.mkdtempSync(path.join(os.tmpdir(), 'gg-real-'));
    fs.mkdirSync(path.join(real, 'src'));
    fs.writeFileSync(path.join(real, 'src', 'a.ts'), 'x');
    const link = real + '-link';
    fs.symlinkSync(real, link, 'junction'); // a junction on Windows (no rights needed), a symlink elsewhere
    try {
      // the editor opened the file through the link, git names the real folder
      h.docs = [doc(path.join(link, 'src', 'a.ts'), true)];
      await handleWebviewMessage({ type: 'call', id: 11, method: 'switch', params: { ...params, folder: real } }, deps);
      // and the other way round
      h.docs = [doc(path.join(real, 'src', 'a.ts'), true)];
      await handleWebviewMessage({ type: 'call', id: 12, method: 'switch', params: { ...params, folder: link } }, deps);
      expect(call).not.toHaveBeenCalled();
      expect(posted.map((m) => [m.result.reason, m.result.files])).toEqual([
        ['unsaved-editors', ['src/a.ts']],
        ['unsaved-editors', ['src/a.ts']],
      ]);
    } finally {
      fs.rmSync(link, { recursive: false, force: true });
    }
  });

  it('refuses switch while a custom editor under the folder is dirty', async () => {
    const image = { scheme: 'file', fsPath: path.join(root, 'img', 'logo.png') };
    h.tabs = [
      { isDirty: true, input: { uri: image, viewType: 'imagePreview' } },
      { isDirty: true, input: { original: { scheme: 'git', fsPath: '/x' }, modified: { scheme: 'file', fsPath: path.join(root, 'b.ts') } } },
      { isDirty: true, input: undefined }, // a terminal or webview: nothing to place
      { isDirty: false, input: { uri: { scheme: 'file', fsPath: path.join(root, 'saved.ts') } } },
    ];
    h.docs = [doc(path.join(root, 'b.ts'), true)]; // the same file as the diff tab: listed once
    await handleWebviewMessage({ type: 'call', id: 13, method: 'switch', params }, deps);
    expect(call).not.toHaveBeenCalled();
    expect(posted[0].result).toMatchObject({ ok: false, reason: 'unsaved-editors', files: ['b.ts', 'img/logo.png'] });
  });

  it('forwards only the keys it checked, so another spelling of folder cannot slip past', async () => {
    const clean = fs.mkdtempSync(path.join(os.tmpdir(), 'gg-clean-'));
    h.docs = [doc(path.join(root, 'src', 'login.ts'), true)];
    // the core reads keys without case and keeps the last of duplicate keys
    const sneaky = { ...params, folder: clean, Folder: root, FOLDER: root, extra: 1 };
    await handleWebviewMessage({ type: 'call', id: 14, method: 'switch', params: sneaky }, deps);
    expect(call).toHaveBeenCalledTimes(1);
    expect(call.mock.calls[0][1]).toEqual({ ...params, folder: clean });
  });

  it('refuses a switch whose folder is not text, without asking the core', async () => {
    h.docs = [doc(path.join(root, 'src', 'login.ts'), true)];
    const { folder: _folder, ...rest } = params;
    const bad: Array<[number, object | undefined]> = [
      [15, { ...rest, FOLDER: root }],
      [16, { ...rest, folder: 5, Folder: root }],
      [17, { ...rest, folder: '' }],
      [18, undefined],
    ];
    for (const [id, p] of bad) await handleWebviewMessage({ type: 'call', id, method: 'switch', params: p }, deps);
    expect(call).not.toHaveBeenCalled();
    expect(posted.map((m) => [m.id, m.error?.code])).toEqual([
      [15, 'bad-message'],
      [16, 'bad-message'],
      [17, 'bad-message'],
      [18, 'bad-message'],
    ]);
  });

  it('leaves other writes alone', async () => {
    h.docs = [doc(path.join(root, 'src', 'login.ts'), true)];
    await handleWebviewMessage({ type: 'call', id: 9, method: 'align', params: { repo: root } }, deps);
    expect(call).toHaveBeenCalledWith('align', { repo: root }, undefined, undefined);
  });
});

describe('unsavedUnder', () => {
  const d = (fsPath: string, isDirty = true) => ({ uri: { scheme: 'file', fsPath }, isDirty });

  it('compares paths without case on every system', () => {
    const docs = [d('C:\\Work\\Repo\\src\\a.ts'), d('c:/work/repo/b.ts'), d('C:\\Work\\Repo2\\c.ts')];
    expect(unsavedUnder('c:\\work\\repo\\', docs, 'win32')).toEqual(['src/a.ts', 'b.ts']);
    const mac = [d('/Users/dev/repo/a.ts'), d('/Users/dev/Repo/B.ts'), d('/Users/dev/Repo2/c.ts')];
    expect(unsavedUnder('/Users/dev/Repo', mac, 'darwin')).toEqual(['a.ts', 'B.ts']);
    expect(unsavedUnder('/home/dev/Repo', [d('/home/dev/repo/a.ts'), d('/home/dev/Repo/b.ts')], 'linux')).toEqual(['a.ts', 'b.ts']);
  });

  it('never resolves a network path', () => {
    const real = vi.fn((p: string) => p);
    const docs = [d('//host/share/repo/a.ts'), d('/home/dev/other/b.ts')];
    expect(unsavedUnder('//host/share/repo', docs, 'linux', real)).toEqual(['a.ts']);
    expect(unsavedUnder('\\\\host\\share\\repo', [d('\\\\host\\share\\repo\\c.ts')], 'win32', real)).toEqual(['c.ts']);
    expect(real.mock.calls.map((c) => c[0])).toEqual(['/home/dev/other/b.ts']);
  });

  it('skips saved and non-file documents', () => {
    expect(unsavedUnder('/w/r', [d('/w/r/a.ts', false), { uri: { scheme: 'untitled', fsPath: '/w/r/x' }, isDirty: true }], 'linux')).toEqual([]);
  });
});

describe('handleWebviewMessage', () => {
  it('forwards call and answers by id', async () => {
    await handleWebviewMessage({ type: 'call', id: 4, method: 'view', params: { repo: 'r' } }, deps);
    expect(call).toHaveBeenCalledWith('view', { repo: 'r' }, undefined, undefined);
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
  const I = 'inst-1'; // the page instance: made by the page each time it loads

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
    const pending = handlePageMessage({ type: 'call', id: 1, method: 'view', page: old, instance: I }, gen, deps);
    const fresh = gen.next(); // the page was reloaded
    finish({ old: true });
    await pending;
    expect(posted).toEqual([]);
    // a call made after the reload is answered
    await handlePageMessage({ type: 'call', id: 1, method: 'view', page: fresh, instance: 'inst-2' }, gen, deps);
    expect(posted).toEqual([{ type: 'result', id: 1, result: { ok: true } }]);
  });

  it('ignores, and logs by type only, a message of an older render that arrives after the reload', async () => {
    const lines: string[] = [];
    const gen = new Generation();
    const old = gen.next();
    gen.next();
    const d = { ...deps, log: (l: string) => lines.push(l) };
    await handlePageMessage({ type: 'call', id: 1, method: 'view', params: { repo: '/secret/path' }, page: old, instance: I }, gen, d);
    await handlePageMessage({ type: 'copy', text: 'x', page: old, instance: I }, gen, d);
    await handlePageMessage({ type: 'openWide', page: old, instance: I }, gen, d);
    expect(call).not.toHaveBeenCalled();
    expect(h.writeText).not.toHaveBeenCalled();
    expect(openWide).not.toHaveBeenCalled();
    expect(posted).toEqual([]);
    expect(lines).toHaveLength(3);
    expect(lines[0]).toMatch(/ignored a call message/);
    expect(lines.join('\n')).not.toContain('/secret/path');
    expect(lines.join('\n')).not.toContain(old);
  });

  it('ignores a message without a token or without an instance', async () => {
    const gen = new Generation();
    const page = gen.next();
    await handlePageMessage({ type: 'call', id: 1, method: 'view', instance: I }, gen, deps);
    await handlePageMessage({ type: 'call', id: 2, method: 'view', page: 7, instance: I }, gen, deps);
    await handlePageMessage({ type: 'call', id: 3, method: 'view', page }, gen, deps);
    expect(call).not.toHaveBeenCalled();
    expect(posted).toEqual([]);
  });

  it('serves a message of the current render', async () => {
    const gen = new Generation();
    const page = gen.next();
    await handlePageMessage({ type: 'call', id: 3, method: 'repos', page, instance: I }, gen, deps);
    expect(call).toHaveBeenCalledWith('repos', undefined, undefined, expect.any(AbortSignal));
    expect(posted).toEqual([{ type: 'result', id: 3, result: { ok: true } }]);
  });

  it('never gives a page that reloaded with the same HTML a reply meant for the instance before it', async () => {
    let finish!: (v: unknown) => void;
    call.mockImplementationOnce(() => new Promise((r) => (finish = r)));
    const gen = new Generation();
    const page = gen.next();
    const pending = handlePageMessage({ type: 'call', id: 1, method: 'view', page, instance: 'first' }, gen, deps);
    // VS Code reloaded the webview: same HTML, same token, a new instance whose ids start at 1 again
    await handlePageMessage({ type: 'call', id: 1, method: 'repos', page, instance: 'second' }, gen, deps);
    finish({ meantFor: 'first' });
    await pending;
    expect(posted).toEqual([{ type: 'result', id: 1, result: { ok: true } }]);
    // the first instance is gone for good: its messages are ignored
    await handlePageMessage({ type: 'call', id: 2, method: 'view', page, instance: 'first' }, gen, deps);
    expect(call).toHaveBeenCalledTimes(2);
  });

  it('cancels the reads in flight of a render when the page is rendered again or goes away', async () => {
    const signals: AbortSignal[] = [];
    call.mockImplementation((_m: string, _p: unknown, _t: unknown, signal: AbortSignal) => {
      signals.push(signal);
      return new Promise(() => undefined);
    });
    const gen = new Generation();
    const page = gen.next();
    void handlePageMessage({ type: 'call', id: 1, method: 'view', page, instance: I }, gen, deps);
    await Promise.resolve();
    expect(signals[0].aborted).toBe(false);
    const next = gen.next();
    expect(signals[0].aborted).toBe(true);
    void handlePageMessage({ type: 'call', id: 1, method: 'view', page: next, instance: 'b' }, gen, deps);
    await Promise.resolve();
    expect(signals[1].aborted).toBe(false);
    void handlePageMessage({ type: 'call', id: 2, method: 'view', page: next, instance: 'c' }, gen, deps);
    await Promise.resolve();
    expect(signals[1].aborted).toBe(true); // instance b was replaced by c
    gen.dispose();
    expect(signals[2].aborted).toBe(true);
  });
});

describe('logging calls that failed for a page', () => {
  it('logs method, code and elapsed time, never the params', async () => {
    const lines: string[] = [];
    call.mockRejectedValueOnce(Object.assign(new Error('view did not answer within 300 s'), { code: 'timeout' }));
    await handleWebviewMessage({ type: 'call', id: 1, method: 'view', params: { repo: '/private/repo' } }, { ...deps, log: (l) => lines.push(l) });
    expect(lines).toHaveLength(1);
    expect(lines[0]).toMatch(/^view failed: timeout after \d+ ms$/);
    expect(posted[0]).toMatchObject({ id: 1, error: { code: 'timeout' } });
  });

  it('says nothing and answers nothing for a call cancelled because its page went away', async () => {
    const lines: string[] = [];
    call.mockRejectedValueOnce(Object.assign(new Error('cancelled'), { code: 'cancelled' }));
    await handleWebviewMessage({ type: 'call', id: 1, method: 'view' }, { ...deps, log: (l) => lines.push(l) });
    expect(lines).toEqual([]);
    expect(posted).toEqual([]);
  });
});

describe('repository list during startup discovery', () => {
  it('asks the core for repos only after beforeRepos settles, and sends the discovery problems first', async () => {
    let open!: (p: unknown[]) => void;
    const problems = [{ folder: '/w', code: 'core-unavailable', message: 'x' }];
    const beforeRepos = vi.fn(() => new Promise<any[]>((r) => (open = r)));
    const pending = handleWebviewMessage({ type: 'call', id: 1, method: 'repos' }, { ...deps, beforeRepos });
    await Promise.resolve();
    expect(beforeRepos).toHaveBeenCalledTimes(1);
    expect(call).not.toHaveBeenCalled();
    open(problems);
    await pending;
    expect(call).toHaveBeenCalledWith('repos', undefined, undefined, undefined);
    expect(posted).toEqual([
      { type: 'discovery', problems },
      { type: 'result', id: 1, result: { ok: true } },
    ]);
  });

  it('does not hold other methods', async () => {
    const beforeRepos = vi.fn(() => new Promise<any[]>(() => undefined));
    await handleWebviewMessage({ type: 'call', id: 1, method: 'view', params: { repo: '/r' } }, { ...deps, beforeRepos });
    expect(beforeRepos).not.toHaveBeenCalled();
    expect(call).toHaveBeenCalledWith('view', { repo: '/r' }, undefined, undefined);
  });
});
