import { HostError } from './types';
import type { Host, ThemeKind } from './types';
import type { Global, RpcResponse } from '../types';

const TOKEN_KEY = 'gitglance.token';

// loadToken takes the token from the page address (#token=<t>), keeps it in
// sessionStorage and removes it from the address bar.
function loadToken(): string {
  const m = /(?:^#|&)token=([^&]+)/.exec(location.hash);
  if (m) {
    const token = decodeURIComponent(m[1]);
    try {
      sessionStorage.setItem(TOKEN_KEY, token);
    } catch {
      // storage can be blocked; the token then only lives in memory
    }
    history.replaceState(null, '', location.pathname + location.search);
    return token;
  }
  try {
    return sessionStorage.getItem(TOKEN_KEY) ?? '';
  } catch {
    return '';
  }
}

// browserHost talks to `gitglance serve` with POST /rpc. There are no cookies;
// the token travels in a header.
export function browserHost(): Host {
  const token = loadToken();
  const dark = window.matchMedia?.('(prefers-color-scheme: dark)');
  const listeners: Array<(t: ThemeKind) => void> = [];
  let setting: Global['theme'] = 'auto';
  let nextId = 1;

  const theme = (): ThemeKind => {
    if (setting === 'dark' || setting === 'light') return setting;
    return dark && !dark.matches ? 'light' : 'dark';
  };
  const notify = () => listeners.forEach((cb) => cb(theme()));

  const call = async <T>(method: string, params?: object): Promise<T> => {
    if (!token) throw new HostError('no-token', 'no access token: open the link printed in the terminal');
    const res = await fetch('/rpc', {
      method: 'POST',
      credentials: 'omit',
      headers: { 'Content-Type': 'application/json', 'X-GitGlance-Token': token },
      body: JSON.stringify({ id: nextId++, method, params }),
    });
    let body: RpcResponse<T>;
    try {
      body = (await res.json()) as RpcResponse<T>;
    } catch {
      throw new HostError('bad-response', `unexpected response (HTTP ${res.status})`);
    }
    if (body.error) throw new HostError(body.error.code, body.error.message);
    return body.result as T;
  };

  dark?.addEventListener?.('change', () => {
    if (setting === 'auto') notify();
  });
  if (token) {
    // The saved theme setting wins over the system one once it is known.
    void call<Global>('getGlobal')
      .then((g) => {
        if ((g?.theme === 'dark' || g?.theme === 'light' || g?.theme === 'auto') && g.theme !== setting) {
          setting = g.theme;
          notify();
        }
      })
      .catch(() => undefined);
  }

  return {
    call,
    openFolder: async (path) => {
      await call('openInEditor', { path });
    },
    copy: async (text) => navigator.clipboard.writeText(text),
    openDiff: async () => false,
    openWide: () => undefined,
    mode: 'wide',
    locale: navigator.language || 'en',
    theme,
    onTheme: (cb) => {
      listeners.push(cb);
    },
  };
}
