import * as fs from 'node:fs';
import * as path from 'node:path';
import * as vscode from 'vscode';
import type { CoreClient } from './core';
import { gitUri } from './gitfs';

export interface BridgeDeps {
  core: Pick<CoreClient, 'call'>;
  post(message: object): void;
  openWide(): void;
  log(line: string): void;
}

const METHOD_NAME = /^[A-Za-z]{1,40}$/;
const MAX_COPY = 1_000_000;

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
      if (typeof msg.method !== 'string' || !METHOD_NAME.test(msg.method)) return fail('bad-message', 'invalid method');
      if (msg.params !== undefined && !isObject(msg.params)) return fail('bad-message', 'invalid params');
      try {
        const result = await deps.core.call(msg.method, msg.params as object | undefined);
        deps.post({ type: 'result', id, result });
      } catch (e: any) {
        deps.post({ type: 'result', id, error: { code: String(e?.code ?? 'error'), message: String(e?.message ?? e) } });
      }
      return;
    }
    case 'openFolder': {
      const p = msg.path;
      if (!isText(p) || !path.isAbsolute(p)) return fail('bad-path', 'The folder path is not absolute.');
      let isDir = false;
      try {
        isDir = fs.statSync(p).isDirectory();
      } catch {
        isDir = false;
      }
      if (!isDir) return fail('bad-path', 'The folder does not exist.');
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
        return fail('bad-message', 'invalid diff request');
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
