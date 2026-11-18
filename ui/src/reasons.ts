import type { Key, T } from './i18n';
import { HostError } from './host/types';
import { en } from './i18n/en';

export type ReasonKind = 'code' | 'align.skip' | 'switch.reason' | 'create.reason' | 'remove.reason';

// reasonText turns a code the core (or the host) can return into text. A code
// from a newer core that has no text yet is shown as it is.
export function reasonText(kind: ReasonKind, code: string, t: T): string {
  const key = `${kind}.${code}`;
  return key in en ? t(key as Key) : code;
}

// errorText describes a rejected call: the text of its code, then the message
// of the core when there is one.
export function errorText(e: unknown, t: T): string {
  if (e instanceof HostError) {
    const known = reasonText('code', e.code, t);
    if (known === e.code) return e.message ? `${e.code}: ${e.message}` : e.code;
    return e.message && e.code !== 'no-token' ? `${known} (${e.message})` : known;
  }
  return t('error.generic', { message: e instanceof Error ? e.message : String(e) });
}

// baseName is the last segment of a path written with / or \.
export function baseName(path: string): string {
  const parts = path.split(/[\\/]+/).filter(Boolean);
  return parts[parts.length - 1] ?? path;
}
