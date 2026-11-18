import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { browserHost } from './browser';

// The host asks for the saved theme at start; tests look at the call they care about.
function callOf(f: { mock: { calls: unknown[][] } }, method: string): [string, any] {
  const hit = f.mock.calls.find((c) => JSON.parse((c[1] as any).body).method === method);
  if (!hit) throw new Error('no call to ' + method);
  return hit as [string, any];
}

function reply(body: unknown) {
  return vi.fn(async (..._args: unknown[]) => new Response(JSON.stringify(body), { status: 200 }));
}

beforeEach(() => {
  sessionStorage.clear();
  history.replaceState(null, '', '/');
});
afterEach(() => {
  vi.unstubAllGlobals();
});

describe('browserHost', () => {
  it('reads the token from the hash, stores it and strips the hash', () => {
    history.replaceState(null, '', '/#token=abc123');
    browserHost();
    expect(sessionStorage.getItem('gitglance.token')).toBe('abc123');
    expect(location.hash).toBe('');
  });

  it('keeps using the stored token after a reload', async () => {
    sessionStorage.setItem('gitglance.token', 'kept');
    const f = reply({ id: 1, result: 1 });
    vi.stubGlobal('fetch', f);
    await browserHost().call('version');
    expect(callOf(f, 'version')[1].headers['X-GitGlance-Token']).toBe('kept');
  });

  it('sends both headers and omits credentials', async () => {
    history.replaceState(null, '', '/#token=abc123');
    const f = reply({ id: 1, result: { core: '1' } });
    vi.stubGlobal('fetch', f);
    const out = await browserHost().call<{ core: string }>('version', { x: 1 });
    expect(out).toEqual({ core: '1' });
    const [url, init] = callOf(f, 'version');
    expect(url).toBe('/rpc');
    expect(init.method).toBe('POST');
    expect(init.credentials).toBe('omit');
    expect(init.headers['Content-Type']).toBe('application/json');
    expect(init.headers['X-GitGlance-Token']).toBe('abc123');
    expect(JSON.parse(init.body)).toMatchObject({ method: 'version', params: { x: 1 } });
  });

  it('rejects with no-token when there is no token and sends nothing', async () => {
    const f = reply({});
    vi.stubGlobal('fetch', f);
    await expect(browserHost().call('version')).rejects.toMatchObject({ code: 'no-token' });
    expect(f).not.toHaveBeenCalled();
  });

  it('rejects with the code and message of a server error', async () => {
    history.replaceState(null, '', '/#token=t');
    vi.stubGlobal('fetch', reply({ id: 1, error: { code: 'not-repo', message: 'nope' } }));
    await expect(browserHost().call('view', { repo: '/x' })).rejects.toMatchObject({ code: 'not-repo', message: 'nope' });
  });

  it('openDiff returns false and openWide does nothing', async () => {
    const h = browserHost();
    expect(await h.openDiff('/r', 'a', 'b', 'p')).toBe(false);
    expect(() => h.openWide()).not.toThrow();
  });
});
