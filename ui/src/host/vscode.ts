import { HostError } from './types';
import type { DiscoveryProblem, Host, ThemeKind } from './types';

type Pending = { resolve: (v: any) => void; reject: (e: Error) => void };

// readInit finds the init object: window.__GG_INIT__ if a script set it, else the
// JSON in <meta name="gg-init"> (the extension cannot use inline scripts under its CSP).
function readInit(): NonNullable<Window['__GG_INIT__']> {
  if (window.__GG_INIT__) return window.__GG_INIT__;
  try {
    const raw = document.querySelector('meta[name="gg-init"]')?.getAttribute('content');
    if (raw) return JSON.parse(raw);
  } catch {
    // fall through to the defaults
  }
  return {};
}

// randomId is 128 random bits as hex.
const randomId = () => Array.from(crypto.getRandomValues(new Uint8Array(16)), (b) => b.toString(16).padStart(2, '0')).join('');

const isProblem = (p: unknown): p is DiscoveryProblem =>
  !!p && typeof p === 'object' && ['folder', 'code', 'message'].every((k) => typeof (p as Record<string, unknown>)[k] === 'string');

// vscodeHost talks to the extension through webview postMessage. The extension
// forwards calls to the core and answers with {type:'result', id, result|error}.
// Every message carries the page token of this render and an instance id this
// load of the page makes for itself: the extension ignores messages of a page
// it has replaced since, even one VS Code reloaded with the same HTML.
export function vscodeHost(): Host {
  const api = window.acquireVsCodeApi!();
  const init = readInit();
  const pending = new Map<number, Pending>();
  const listeners: Array<(t: ThemeKind) => void> = [];
  const refreshers: Array<() => void> = [];
  const discoveries: Array<(problems: DiscoveryProblem[]) => void> = [];
  const instance = randomId();
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
    } else if (m.type === 'refresh') {
      refreshers.forEach((cb) => cb());
    } else if (m.type === 'discovery' && Array.isArray(m.problems)) {
      const problems = m.problems.filter(isProblem);
      discoveries.forEach((cb) => cb(problems));
    }
  });

  const post = (message: object) => api.postMessage({ ...message, page: init.page, instance });

  const request = <T>(message: object) =>
    new Promise<T>((resolve, reject) => {
      const id = nextId++;
      pending.set(id, { resolve, reject });
      post({ ...message, id });
    });

  return {
    call: <T>(method: string, params?: object) => request<T>({ type: 'call', method, params }),
    openFolder: async (path) => post({ type: 'openFolder', path }),
    copy: async (text) => post({ type: 'copy', text }),
    openDiff: (repo, a, b, path) => request<boolean>({ type: 'openDiff', repo, a, b, path }),
    openWide: () => post({ type: 'openWide' }),
    mode: init.mode ?? 'sidebar',
    locale: init.locale ?? 'en',
    theme: () => current,
    onTheme: (cb) => {
      listeners.push(cb);
    },
    onRefresh: (cb) => {
      refreshers.push(cb);
    },
    onDiscovery: (cb) => {
      discoveries.push(cb);
    },
  };
}
