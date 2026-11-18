import * as vscode from 'vscode';
import { alignAll } from './align';
import { Generation, handlePageMessage } from './bridge';
import { coreBinaryPath } from './binpath';
import { CoreClient } from './core';
import { GIT_SCHEME, GitContentProvider } from './gitfs';
import { Discovery } from './startup';
import { renderHtml } from './webview';

type Mode = 'sidebar' | 'wide';

// How long a page that asks for the repository list waits for startup discovery.
const DISCOVERY_WAIT_MS = 15_000;

const themeKind = (): 'light' | 'dark' => {
  const k = vscode.window.activeColorTheme.kind;
  return k === vscode.ColorThemeKind.Light || k === vscode.ColorThemeKind.HighContrastLight ? 'light' : 'dark';
};

let activeCore: CoreClient | undefined;

// What a page and the extension said to each other; kept only in the test host.
export interface TrafficEntry {
  mode: Mode;
  dir: 'in' | 'out';
  message: unknown;
}

interface Page {
  mode: Mode;
  render(): void;
  post(message: object): void;
}

export function activate(ctx: vscode.ExtensionContext) {
  const out = vscode.window.createOutputChannel('Git Glance');
  const core = new CoreClient(coreBinaryPath(ctx.extensionPath), undefined, ctx.extensionPath);
  activeCore = core;
  core.onLog((l) => out.appendLine(l));
  const log = (l: string) => out.appendLine(l);
  const testing = ctx.extensionMode === vscode.ExtensionMode.Test;
  const traffic: TrafficEntry[] = [];
  const record = (mode: Mode, dir: 'in' | 'out', message: unknown) => {
    if (testing) traffic.push({ mode, dir, message });
  };

  const pages = new Map<vscode.Webview, Page>();
  let wide: vscode.WebviewPanel | undefined;
  // A page reads the repository list again (and its current view) on "refresh".
  const refreshPage = (webview: vscode.Webview) => pages.get(webview)?.post({ type: 'refresh' });
  const refreshPages = () => pages.forEach((p) => p.post({ type: 'refresh' }));

  const discovery = new Discovery(registerWorkspaceRepos(core, log), DISCOVERY_WAIT_MS);

  const setup = (webview: vscode.Webview, mode: Mode): vscode.Disposable => {
    const gen = new Generation();
    webview.options = { enableScripts: true, localResourceRoots: [vscode.Uri.joinPath(ctx.extensionUri, 'media', 'ui')] };
    const post = (x: object) => {
      record(mode, 'out', x);
      void webview.postMessage(x);
    };
    const render = () => {
      const init = { locale: vscode.env.language, theme: themeKind(), page: gen.next() };
      try {
        webview.html = renderHtml(webview, ctx.extensionUri, mode, init);
      } catch (e) {
        log(`cannot render the page: ${e}`);
        webview.html = `<!doctype html><body>${vscode.l10n.t('Git Glance could not load its page.')}</body>`;
      }
    };
    const deps = {
      core,
      post,
      openWide,
      log,
      beforeRepos: () => discovery.beforeRepos(webview, () => refreshPage(webview)),
    };
    pages.set(webview, { mode, render, post });
    render();
    return webview.onDidReceiveMessage((m) => {
      record(mode, 'in', m);
      handlePageMessage(m, gen, deps).catch((e) => log(`bridge: ${e}`));
    });
  };

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
    const subscription = setup(panel.webview, 'wide');
    panel.onDidDispose(() => {
      subscription.dispose();
      pages.delete(panel.webview);
      wide = undefined;
    });
  }

  const sidebar: vscode.WebviewViewProvider = {
    resolveWebviewView(view) {
      const subscription = setup(view.webview, 'sidebar');
      view.onDidDispose(() => {
        subscription.dispose();
        pages.delete(view.webview);
      });
    },
  };

  ctx.subscriptions.push(
    out,
    vscode.window.registerWebviewViewProvider('gitglance.sidebar', sidebar, { webviewOptions: { retainContextWhenHidden: true } }),
    vscode.workspace.registerTextDocumentContentProvider(GIT_SCHEME, new GitContentProvider(core)),
    vscode.commands.registerCommand('gitglance.openWide', openWide),
    // Refresh starts every page from scratch, and lets a core that failed to
    // start be started again.
    vscode.commands.registerCommand('gitglance.refresh', () => {
      core.allowRestart();
      pages.forEach((p) => p.render());
    }),
    vscode.commands.registerCommand('gitglance.alignAll', () => alignAll(core, log, refreshPages)),
    vscode.window.onDidChangeActiveColorTheme(() => {
      pages.forEach((p) => p.post({ type: 'theme', kind: themeKind() }));
    }),
  );

  // Only the test host may reach the core client.
  return testing ? { CoreClient, core: () => core, traffic: () => traffic.slice(), discovered: () => discovery.done } : undefined;
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

// deactivate lets the core finish what it is writing before the host stops.
export function deactivate(): Thenable<void> | undefined {
  return activeCore?.dispose();
}
