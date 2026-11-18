import * as vscode from 'vscode';
import { alignAll } from './align';
import { Generation, handlePageMessage } from './bridge';
import { coreBinaryPath } from './binpath';
import { CoreClient } from './core';
import { GIT_SCHEME, GitContentProvider } from './gitfs';
import { Discovery, registerFolders } from './startup';
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

  const discovery = new Discovery(DISCOVERY_WAIT_MS, log);
  const folders = () => (vscode.workspace.workspaceFolders ?? []).map((f) => f.uri.fsPath);
  const discover = () => discovery.run(() => registerFolders(core, folders(), log));
  discover();

  // setup renders a page into webview and serves its messages; disposing the
  // result stops both and cancels the page's reads.
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
    const listening = webview.onDidReceiveMessage((m) => {
      record(mode, 'in', m);
      handlePageMessage(m, gen, deps).catch((e) => log(`bridge: ${e}`));
    });
    return new vscode.Disposable(() => {
      listening.dispose();
      gen.dispose();
      pages.delete(webview);
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
      wide = undefined;
    });
  }

  const sidebar: vscode.WebviewViewProvider = {
    resolveWebviewView(view) {
      const subscription = setup(view.webview, 'sidebar');
      view.onDidDispose(() => subscription.dispose());
    },
  };

  ctx.subscriptions.push(
    out,
    vscode.window.registerWebviewViewProvider('gitglance.sidebar', sidebar, { webviewOptions: { retainContextWhenHidden: true } }),
    vscode.workspace.registerTextDocumentContentProvider(GIT_SCHEME, new GitContentProvider(core)),
    vscode.commands.registerCommand('gitglance.openWide', openWide),
    // Refresh lets a core that failed to start be started again, registers the
    // workspace repositories again (the pages' repository lists wait for it)
    // and starts every page from scratch.
    vscode.commands.registerCommand('gitglance.refresh', () => {
      core.allowRestart();
      discover();
      pages.forEach((p) => p.render());
    }),
    vscode.commands.registerCommand('gitglance.alignAll', () =>
      alignAll(core, log, refreshPages, () => discovery.beforeRepos('alignAll', () => undefined)),
    ),
    vscode.window.onDidChangeActiveColorTheme(() => {
      pages.forEach((p) => p.post({ type: 'theme', kind: themeKind() }));
    }),
  );

  // Only the test host may reach the core client.
  return testing ? { CoreClient, core: () => core, traffic: () => traffic.slice(), discovered: () => discovery.done } : undefined;
}

// deactivate lets the core finish what it is writing before the host stops.
export function deactivate(): Thenable<void> | undefined {
  return activeCore?.dispose();
}
