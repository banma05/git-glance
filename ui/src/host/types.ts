import { browserHost } from './browser';
import { vscodeHost } from './vscode';

export type ThemeKind = 'dark' | 'light';

export interface Host {
  call<T>(method: string, params?: object): Promise<T>;
  openFolder(path: string): Promise<void>;
  copy(text: string): Promise<void>;
  openDiff(repo: string, a: string, b: string, path: string): Promise<boolean>; // false: the UI shows fileDiff in the page
  openWide(): void; // no-op in the browser
  mode: 'sidebar' | 'wide';
  locale: string; // e.g. "zh-cn", "en"
  theme(): ThemeKind;
  onTheme(cb: (t: ThemeKind) => void): void;
}

// HostError is what a rejected call carries: the code and message of the core's
// error, or a code made up by the host itself such as "no-token".
export class HostError extends Error {
  code: string;
  constructor(code: string, message: string) {
    super(message);
    this.name = 'HostError';
    this.code = code;
  }
}

declare global {
  interface Window {
    acquireVsCodeApi?: () => { postMessage(message: unknown): void };
    __GG_INIT__?: { mode?: 'sidebar' | 'wide'; locale?: string; theme?: ThemeKind };
  }
}

export function pickHost(): Host {
  return window.acquireVsCodeApi ? vscodeHost() : browserHost();
}
