import { describe, expect, it, vi } from 'vitest';

vi.mock('vscode', () => ({ Uri: { joinPath: (...a: unknown[]) => a } }));

import { CSP_TEMPLATE, transformHtml } from './webview';

const html = `<!doctype html><html><head><meta charset="UTF-8" />
<script type="module" crossorigin src="./assets/index-a.js"></script>
<link rel="stylesheet" crossorigin href="./assets/index-b.css">
</head><body><div id="app"></div></body></html>`;

describe('transformHtml', () => {
  const out = transformHtml(html, (p) => `https://res/${p}`, 'https://csp', { mode: 'wide', locale: 'en', theme: 'dark' });

  it('rewrites asset urls', () => {
    expect(out).toContain('src="https://res/assets/index-a.js"');
    expect(out).toContain('href="https://res/assets/index-b.css"');
  });

  it('adds the exact CSP and no inline script', () => {
    expect(CSP_TEMPLATE).toBe(
      "default-src 'none'; script-src ${cspSource}; style-src ${cspSource} 'unsafe-inline'; img-src ${cspSource} data:; font-src ${cspSource}",
    );
    const csp = CSP_TEMPLATE.split('${cspSource}').join('https://csp');
    expect(out).toContain(`http-equiv="Content-Security-Policy" content="${csp}"`);
    expect(out).not.toMatch(/<script(?![^>]*\bsrc=)[^>]*>/);
  });

  it('carries the init object in a meta tag', () => {
    const m = out.match(/<meta name="gg-init" content="([^"]*)"/)!;
    const json = m[1].replace(/&quot;/g, '"').replace(/&amp;/g, '&');
    expect(JSON.parse(json)).toEqual({ mode: 'wide', locale: 'en', theme: 'dark' });
  });
});
