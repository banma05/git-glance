import { HostError } from './types';
import type { Host, ThemeKind } from './types';

type Pending = { resolve: (v: any) => void; reject: (e: Error) => void };

// vscodeHost talks to the extension through webview postMessage. The extension
// forwards calls to the core and answers with {type:'result', id, result|error}.
export function vscodeHost(): Host {
  const api = window.acquireVsCodeApi!();
  const init = window.__GG_INIT__ ?? {};
  const pending = new Map<number, Pending>();
  const listeners: Array<(t: ThemeKind) => void> = [];
  let current: ThemeKind = init.theme ?? 'dark';
  let nextId = 1;

  window.addEventListener('message', (ev: MessageEvent) => {
    const m = ev.data;
    if (!m || typeof m !== 'object') return;
    if (m.type === 'result') {
      const p = pending.get(m.id);
      if (!p) return;
      pending.delete(m.id);
      if (m.error) p.reject(new HostError(m.error.code, m.error.message));
      else p.resolve(m.result);
    } else if (m.type === 'theme') {
      current = m.kind === 'light' ? 'light' : 'dark';
      listeners.forEach((cb) => cb(current));
    }
  });

  const request = <T>(message: object) =>
    new Promise<T>((resolve, reject) => {
      const id = nextId++;
      pending.set(id, { resolve, reject });
      api.postMessage({ ...message, id });
    });

  return {
    call: <T>(method: string, params?: object) => request<T>({ type: 'call', method, params }),
    openFolder: async (path) => api.postMessage({ type: 'openFolder', path }),
    copy: async (text) => api.postMessage({ type: 'copy', text }),
    openDiff: (repo, a, b, path) => request<boolean>({ type: 'openDiff', repo, a, b, path }),
    openWide: () => api.postMessage({ type: 'openWide' }),
    mode: init.mode ?? 'sidebar',
    locale: init.locale ?? 'en',
    theme: () => current,
    onTheme: (cb) => {
      listeners.push(cb);
    },
  };
}
