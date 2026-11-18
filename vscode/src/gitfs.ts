import * as vscode from 'vscode';
import type { CoreClient } from './core';

export const GIT_SCHEME = 'gitglance-git';

interface FileText {
  text: string;
  binary: boolean;
  truncated: boolean;
}

// gitUri names one file at one ref of one repository.
export function gitUri(repo: string, ref: string, path: string): vscode.Uri {
  const query = new URLSearchParams({ repo, ref, path }).toString();
  return vscode.Uri.from({ scheme: GIT_SCHEME, path: '/' + path, query });
}

// GitContentProvider serves file contents from the core's fileAt. It is a
// content provider, so the documents are read-only and nothing is written.
export class GitContentProvider implements vscode.TextDocumentContentProvider {
  constructor(private readonly core: Pick<CoreClient, 'call'>) {}

  async provideTextDocumentContent(uri: vscode.Uri): Promise<string> {
    const q = new URLSearchParams(uri.query);
    const repo = q.get('repo') ?? '';
    const ref = q.get('ref') ?? '';
    const path = q.get('path') ?? '';
    try {
      const f = await this.core.call<FileText>('fileAt', { repo, ref, path });
      if (f.binary) return vscode.l10n.t('Binary file: content is not shown.') + '\n';
      if (f.truncated) return vscode.l10n.t('This file is too large to show here.') + '\n';
      return f.text;
    } catch (e: any) {
      // A file that does not exist on one side of a diff is shown as empty.
      const msg = String(e?.message ?? e);
      if (/does not exist|exists on disk, but not in|not in the tree|bad object|path .* not/i.test(msg)) return '';
      return vscode.l10n.t('Cannot read this file: {0}', msg) + '\n';
    }
  }
}
