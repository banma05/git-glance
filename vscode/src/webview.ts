import * as fs from 'node:fs';
import * as vscode from 'vscode';

// The policy of the pages. ${cspSource} is replaced by the webview's own origin.
export const CSP_TEMPLATE =
  "default-src 'none'; script-src ${cspSource}; style-src ${cspSource} 'unsafe-inline'; img-src ${cspSource} data:; font-src ${cspSource}";

const escapeAttr = (s: string) => s.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

// transformHtml turns the built index.html into a page the webview may load:
// asset urls point at the webview origin, the policy is set, and the init object
// travels in a meta tag (the UI reads it; an inline script would break the policy).
export function transformHtml(html: string, toUri: (relative: string) => string, cspSource: string, init: object): string {
  const csp = CSP_TEMPLATE.split('${cspSource}').join(cspSource);
  const rewritten = html.replace(/(\s(?:src|href)=")\.\/([^"]+)(")/g, (_m, a, rel, z) => `${a}${toUri(rel)}${z}`);
  const head =
    `<meta http-equiv="Content-Security-Policy" content="${escapeAttr(csp)}">` +
    `<meta name="gg-init" content="${escapeAttr(JSON.stringify(init))}">`;
  if (!/<head>/i.test(rewritten)) throw new Error('index.html has no <head>: refusing to build a page without a CSP');
  return rewritten.replace(/<head>/i, (m) => m + head);
}

// renderHtml builds the page for one webview from media/ui/index.html.
export function renderHtml(webview: vscode.Webview, extUri: vscode.Uri, mode: 'sidebar' | 'wide', init: object): string {
  const root = vscode.Uri.joinPath(extUri, 'media', 'ui');
  const html = fs.readFileSync(vscode.Uri.joinPath(root, 'index.html').fsPath, 'utf8');
  const toUri = (rel: string) => webview.asWebviewUri(vscode.Uri.joinPath(root, ...rel.split('/'))).toString();
  return transformHtml(html, toUri, webview.cspSource, { ...init, mode });
}
