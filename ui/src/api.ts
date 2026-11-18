import type { Host } from './host/types';
import type { Methods } from './types';

// rpc calls a core method through the host with the result type of that method.
export function rpc<K extends keyof Methods>(host: Host, method: K, params?: object): Promise<Methods[K]> {
  return host.call<Methods[K]>(method, params);
}
