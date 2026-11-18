import * as vscode from 'vscode';
import { handleWebviewMessage } from './bridge';
import { coreBinaryPath } from './binpath';
import { CoreClient } from './core';
import { GIT_SCHEME, GitContentProvider } from './gitfs';
import { renderHtml } from './webview';

type Mode = 'sidebar' | 'wide';

const themeKind = (): 'light' | 'dark' => {
  const k = vscode.window.activeColorTheme.kind;
  return k === vscode.ColorThemeKind.Light || k === vscode.ColorThemeKind.HighContrastLight ? 'light' : 'dark';
};

let activeCore: CoreClient | undefined;

export function activate(ctx: vscode.ExtensionContext) {
  const out = vscode.window.createOutputChannel('Git Glance');
  const core = new CoreClient(coreBinaryPath(ctx.extensionPath));
  activeCore = core;
  core.onLog((l) => out.appendLine(l));
  const log = (l: string) => out.appendLine(l);

  const pages = new Set<vscode.Webview>();
  let wide: vscode.WebviewPanel | undefined;
  let reloads = 0;

  const initFor = () => ({ locale: vscode.env.language, theme: themeKind(), reload: reloads });
  const setup = (webview: vscode.Webview, mode: Mode) => {
    webview.options = { enableScripts: true, localResourceRoots: [vscode.Uri.joinPath(ctx.extensionUri, 'media', 'ui')] };
    const render = () => {
      try {
        webview.html = renderHtml(webview, ctx.extensionUri, mode, initFor());
      } catch (e) {
        log(`cannot render the page: ${e}`);
        webview.html = '<!doctype html><body>Git Glance could not load its page.</body>';
      }
    };
    render();
    pages.add(webview);
    return {
      render,
      subscription: webview.onDidReceiveMessage((m) =>
        handleWebviewMessage(m, { core, post: (x) => void webview.postMessage(x), openWide, log }).catch((e) => log(`bridge: ${e}`)),
      ),
    };
  };

  const renders = new Map<vscode.Webview, () => void>();

  function openWide() {
    if (wide) {
      wide.reveal();
      return;
    }
    const panel = vscode.window.createWebviewPanel('gitglance.wide', 'Git Glance', vscode.ViewColumn.Active, {
      enableScripts: true,
      retainContextWhenHidden: true,
      localResourceRoots: [vscode.Uri.joinPath(ctx.extensionUri, 'media', 'ui')],
    });
    wide = panel;
    const s = setup(panel.webview, 'wide');
    renders.set(panel.webview, s.render);
    panel.onDidDispose(() => {
      s.subscription.dispose();
      pages.delete(panel.webview);
      renders.delete(panel.webview);
      wide = undefined;
    });
  }

  const sidebar: vscode.WebviewViewProvider = {
    resolveWebviewView(view) {
      const s = setup(view.webview, 'sidebar');
      renders.set(view.webview, s.render);
      view.onDidDispose(() => {
        s.subscription.dispose();
        pages.delete(view.webview);
        renders.delete(view.webview);
      });
    },
  };

  ctx.subscriptions.push(
    out,
    vscode.window.registerWebviewViewProvider('gitglance.sidebar', sidebar, { webviewOptions: { retainContextWhenHidden: true } }),
    vscode.workspace.registerTextDocumentContentProvider(GIT_SCHEME, new GitContentProvider(core)),
    vscode.commands.registerCommand('gitglance.openWide', openWide),
    vscode.commands.registerCommand('gitglance.refresh', () => {
      reloads++;
      renders.forEach((r) => r());
    }),
    vscode.commands.registerCommand('gitglance.alignAll', () => alignAll(core, log)),
    vscode.window.onDidChangeActiveColorTheme(() => {
      for (const w of pages) void w.postMessage({ type: 'theme', kind: themeKind() });
    }),
  );

  void registerWorkspaceRepos(core, log);

  return { CoreClient, core: () => core };
}

// Each workspace folder that is inside a repository is added to the list the
// UI shows. Failures are logged, never thrown.
async function registerWorkspaceRepos(core: CoreClient, log: (l: string) => void): Promise<void> {
  for (const folder of vscode.workspace.workspaceFolders ?? []) {
    try {
      const found = await core.call<{ repo: string }>('discover', { path: folder.uri.fsPath });
      await core.call('addRepo', { path: found.repo });
    } catch (e: any) {
      log(`skipping ${folder.uri.fsPath}: ${e?.message ?? e}`);
    }
  }
}

// alignAll fast-forwards the branches of every listed repository, after the
// user confirms. The core only moves branches that are clean and only behind.
async function alignAll(core: CoreClient, log: (l: string) => void): Promise<void> {
  const yes = 'Align';
  const pick = await vscode.window.showInformationMessage(
    'Fast-forward every branch that is only behind its remote in all listed repositories?',
    { modal: true, detail: 'Branches with local changes or commits of their own are left alone.' },
    yes,
  );
  if (pick !== yes) return;
  let updated = 0;
  let skipped = 0;
  try {
    const repos = await core.call<string[]>('repos');
    for (const repo of repos) {
      try {
        const rep = await core.call<{ updated: unknown[]; skipped: unknown[] }>('align', { repo });
        updated += rep.updated?.length ?? 0;
        skipped += rep.skipped?.length ?? 0;
      } catch (e: any) {
        log(`align failed for ${repo}: ${e?.message ?? e}`);
      }
    }
    void vscode.window.showInformationMessage(`Git Glance: ${updated} branches updated, ${skipped} left alone.`);
  } catch (e: any) {
    void vscode.window.showErrorMessage(`Git Glance: ${e?.message ?? e}`);
  }
}

// deactivate lets the core finish what it is writing before the host stops.
export function deactivate(): Thenable<void> | undefined {
  return activeCore?.dispose();
}
