import { randomBytes } from 'node:crypto';
import * as fs from 'node:fs';
import * as path from 'node:path';
import * as vscode from 'vscode';
import type { CoreClient } from './core';
import { gitUri } from './gitfs';
import type { DiscoveryProblem } from './startup';

export interface BridgeDeps {
  core: Pick<CoreClient, 'call'>;
  post(message: object): void;
  openWide(): void;
  log(line: string): void;
  // holds the repository list while discovery runs, then gives its problems
  beforeRepos?(): Promise<DiscoveryProblem[]>;
  signal?: AbortSignal; // aborts when the page that asked is replaced; reads are then cancelled
}

const METHOD_NAME = /^[A-Za-z]{1,40}$/;
const MESSAGE_TYPE = /^[A-Za-z]{1,20}$/;
const MAX_COPY = 1_000_000;

// Generation tells the renders of one webview apart. Every render gets a new
// token, which travels to the page in its init object, and every load of the
// page makes its own instance id; both come back with each message. A message
// of an older render or of a replaced instance is ignored, and a reply is
// dropped when the render or the instance changed while the call ran, so a
// reloaded page (whose ids restart at 1) never gets an answer meant for the
// page before it, not even when VS Code reloads it with the same HTML. The
// reads of a render or instance that is replaced are cancelled.
export class Generation {
  private current = '';
  private instance = ''; // the page instance that speaks for the current render
  private retired = new Set<string>();
  private reads = new AbortController();

  // next starts a new render and returns its token.
  next(): string {
    this.cancelReads();
    this.current = randomBytes(16).toString('hex');
    this.instance = '';
    this.retired.clear();
    return this.current;
  }

  // dispose ends the last render: its reads are cancelled, no reply is posted.
  dispose(): void {
    this.cancelReads();
    this.current = '';
  }

  // accept returns how to answer a message that carried `page` and `instance`,
  // or undefined when the message does not belong to the current page.
  accept(page: unknown, instance: unknown, post: (message: object) => void): { post(message: object): void; signal: AbortSignal } | undefined {
    if (typeof page !== 'string' || page === '' || page !== this.current) return undefined;
    if (typeof instance !== 'string' || instance === '' || this.retired.has(instance)) return undefined;
    if (instance !== this.instance) {
      // the page was loaded again with the same HTML: the instance before it is gone
      if (this.instance) {
        this.retired.add(this.instance);
        this.cancelReads();
      }
      this.instance = instance;
    }
    return {
      post: (message) => {
        if (page === this.current && instance === this.instance) post(message);
      },
      signal: this.reads.signal,
    };
  }

  private cancelReads(): void {
    this.reads.abort();
    this.reads = new AbortController();
  }
}

// handlePageMessage serves one message of a page if it belongs to the page's
// current render and instance. Others are logged by type only.
export async function handlePageMessage(msg: unknown, gen: Generation, deps: BridgeDeps): Promise<void> {
  const m = isObject(msg) ? msg : {};
  const ok = gen.accept(m.page, m.instance, deps.post);
  if (!ok) {
    const type = typeof m.type === 'string' && MESSAGE_TYPE.test(m.type) ? m.type : 'unknown';
    deps.log(`ignored a ${type} message from a replaced or unknown page`);
    return;
  }
  await handleWebviewMessage(msg, { ...deps, post: ok.post, signal: ok.signal });
}

// unsafeFolderPath is true for paths that must not even be stat-ed: UNC and
// "//host" paths reach out to the network, and on Windows a root-relative path
// without a drive depends on the current drive.
export function unsafeFolderPath(p: string, platform: string = process.platform): boolean {
  if (p.startsWith('//') || p.startsWith('\\\\') || p.startsWith('/\\') || p.startsWith('\\/')) return true;
  return platform === 'win32' && (p.startsWith('/') || p.startsWith('\\'));
}

interface OpenDocument {
  uri: { scheme: string; fsPath: string };
  isDirty: boolean;
}

// unsavedUnder lists, relative to folder and with "/", the open files with
// unsaved changes that live in folder. Paths are compared without case on
// Windows only.
export function unsavedUnder(folder: string, docs: readonly OpenDocument[], platform: string = process.platform): string[] {
  const norm = (p: string) => p.replace(/\\/g, '/').replace(/\/+$/, '');
  const fold = (p: string) => (platform === 'win32' ? p.toLowerCase() : p);
  const root = norm(folder);
  const out: string[] = [];
  for (const d of docs) {
    if (!d.isDirty || d.uri.scheme !== 'file') continue;
    const file = norm(d.uri.fsPath);
    if (fold(file).startsWith(fold(root) + '/')) out.push(file.slice(root.length + 1));
  }
  return out;
}

// unsavedRefusal is the answer to a switch of a folder that has open files with
// unsaved changes: git would change those files under the editor. The core is
// not asked.
function unsavedRefusal(params: unknown): object | undefined {
  const folder = isObject(params) ? params.folder : undefined;
  if (!isText(folder)) return undefined;
  const files = unsavedUnder(folder, vscode.workspace.textDocuments);
  if (files.length === 0) return undefined;
  return {
    ok: false,
    reason: 'unsaved-editors',
    files,
    path: folder,
    detail: '',
    logError: '',
    shelved: null,
    restored: null,
    keptShelf: null,
    fastForwarded: false,
    aheadNote: '',
  };
}

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v);
const isText = (v: unknown, max = 4096): v is string => typeof v === 'string' && v.length > 0 && v.length <= max && !v.includes('\0');

// handleWebviewMessage answers one message from the page. Only the documented
// message types are served; anything else is ignored.
export async function handleWebviewMessage(msg: unknown, deps: BridgeDeps): Promise<void> {
  if (!isObject(msg) || typeof msg.type !== 'string') return;
  const id = typeof msg.id === 'number' && Number.isSafeInteger(msg.id) ? msg.id : undefined;
  const fail = (code: string, message: string) => {
    if (id !== undefined) deps.post({ type: 'result', id, error: { code, message } });
    else void vscode.window.showErrorMessage(message);
  };

  switch (msg.type) {
    case 'call': {
      if (id === undefined) return;
      if (typeof msg.method !== 'string' || !METHOD_NAME.test(msg.method)) return fail('bad-message', vscode.l10n.t('Invalid method.'));
      if (msg.params !== undefined && !isObject(msg.params)) return fail('bad-message', vscode.l10n.t('Invalid parameters.'));
      const refused = msg.method === 'switch' ? unsavedRefusal(msg.params) : undefined;
      if (refused) {
        deps.post({ type: 'result', id, result: refused });
        return;
      }
      const started = Date.now();
      try {
        if (msg.method === 'repos' && deps.beforeRepos) {
          // the problems go first, so the page never shows a plain "no repository" for a failed discovery
          deps.post({ type: 'discovery', problems: await deps.beforeRepos() });
        }
        const result = await deps.core.call(msg.method, msg.params as object | undefined, undefined, deps.signal);
        deps.post({ type: 'result', id, result });
      } catch (e: any) {
        const code = String(e?.code ?? 'error');
        if (code === 'cancelled') return; // the page that asked is gone
        deps.log(`${msg.method} failed: ${code} after ${Date.now() - started} ms`);
        deps.post({ type: 'result', id, error: { code, message: String(e?.message ?? e) } });
      }
      return;
    }
    case 'openFolder': {
      const p = msg.path;
      if (!isText(p) || !path.isAbsolute(p)) return fail('bad-path', vscode.l10n.t('The folder path is not absolute.'));
      if (unsafeFolderPath(p)) return fail('bad-path', vscode.l10n.t('The folder path is not allowed.'));
      let isDir = false;
      try {
        isDir = fs.statSync(p).isDirectory();
      } catch {
        isDir = false;
      }
      if (!isDir) return fail('bad-path', vscode.l10n.t('The folder does not exist.'));
      await vscode.commands.executeCommand('vscode.openFolder', vscode.Uri.file(p), { forceNewWindow: true });
      return;
    }
    case 'copy': {
      if (typeof msg.text !== 'string' || msg.text.length > MAX_COPY) return;
      await vscode.env.clipboard.writeText(msg.text);
      return;
    }
    case 'openDiff': {
      if (id === undefined) return;
      const { repo, a, b, path: file } = msg;
      if (![repo, a, b, file].every((v) => isText(v, 1024)) || String(a).startsWith('-') || String(b).startsWith('-')) {
        return fail('bad-message', vscode.l10n.t('Invalid diff request.'));
      }
      try {
        const title = `${file as string} (${a as string} ↔ ${b as string})`;
        await vscode.commands.executeCommand(
          'vscode.diff',
          gitUri(repo as string, a as string, file as string),
          gitUri(repo as string, b as string, file as string),
          title,
        );
        deps.post({ type: 'result', id, result: true });
      } catch (e: any) {
        deps.log(`openDiff failed: ${e}`);
        deps.post({ type: 'result', id, result: false });
      }
      return;
    }
    case 'openWide':
      deps.openWide();
      return;
    default:
      return;
  }
}
