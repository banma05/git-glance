import { EventEmitter } from 'node:events';
import * as fs from 'node:fs';
import * as path from 'node:path';
import { PassThrough } from 'node:stream';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const l10n = vi.hoisted(() => ({ calls: [] as string[] }));
vi.mock('vscode', () => ({
  l10n: {
    t: (s: string, ...a: unknown[]) => {
      l10n.calls.push(s);
      return s.replace(/\{(\d+)\}/g, (_m, i) => String(a[Number(i)]));
    },
  },
}));

import { CoreClient, WRITE_METHODS } from './core';

class FakeProc extends EventEmitter {
  stdin = new PassThrough();
  stdout = new PassThrough();
  stderr = new PassThrough();
  killed = false;
  stdinEnded = false;
  written: any[] = [];
  constructor() {
    super();
    this.stdin.setEncoding('utf8');
    let buf = '';
    this.stdin.on('data', (d: string) => {
      buf += d;
      let i;
      while ((i = buf.indexOf('\n')) >= 0) {
        this.written.push(JSON.parse(buf.slice(0, i)));
        buf = buf.slice(i + 1);
      }
    });
    this.stdin.on('end', () => (this.stdinEnded = true));
  }
  reply(msg: object) {
    this.stdout.write(JSON.stringify(msg) + '\n');
  }
  exit(code = 1) {
    this.emit('close', code, null);
  }
  kill() {
    this.killed = true;
    this.exit(137);
    return true;
  }
}

function setup() {
  const procs: FakeProc[] = [];
  const spawnFn: any = vi.fn(() => {
    const p = new FakeProc();
    procs.push(p);
    return p;
  });
  return { procs, spawnFn, client: new CoreClient('/bin/gitglance', spawnFn) };
}

const tick = () => new Promise((r) => setImmediate(r));

describe('CoreClient', () => {
  it('spawns "<bin> rpc"', async () => {
    const { client, spawnFn, procs } = setup();
    const p = client.call('version');
    await tick();
    expect(spawnFn.mock.calls[0][0]).toBe('/bin/gitglance');
    expect(spawnFn.mock.calls[0][1]).toEqual(['rpc']);
    procs[0].reply({ id: procs[0].written[0].id, result: 1 });
    expect(await p).toBe(1);
  });

  it('matches out-of-order replies to their calls', async () => {
    const { client, procs } = setup();
    const a = client.call<string>('view', { repo: 'a' });
    const b = client.call<string>('view', { repo: 'b' });
    await tick();
    const [ra, rb] = procs[0].written;
    procs[0].reply({ id: rb.id, result: 'B' });
    procs[0].reply({ id: ra.id, result: 'A' });
    expect(await a).toBe('A');
    expect(await b).toBe('B');
  });

  it('rejects with the core error code', async () => {
    const { client, procs } = setup();
    const p = client.call('view');
    await tick();
    procs[0].reply({ id: procs[0].written[0].id, error: { code: 'not-repo', message: 'nope' } });
    await expect(p).rejects.toMatchObject({ code: 'not-repo', message: 'nope' });
  });

  it('rejects waiting calls when the process exits and restarts once on the next call', async () => {
    const { client, procs, spawnFn } = setup();
    const waiting = client.call('view');
    await tick();
    procs[0].exit(1);
    await expect(waiting).rejects.toMatchObject({ code: 'core-exited' });
    const next = client.call('version');
    await tick();
    expect(spawnFn).toHaveBeenCalledTimes(2);
    procs[1].reply({ id: procs[1].written[0].id, result: 'ok' });
    expect(await next).toBe('ok');
  });

  it('gives up with core-unavailable after two failed starts in a row', async () => {
    const { client, procs, spawnFn } = setup();
    const c1 = client.call('version');
    await tick();
    procs[0].exit(1);
    await expect(c1).rejects.toBeTruthy();
    const c2 = client.call('version');
    await tick();
    procs[1].exit(1);
    await expect(c2).rejects.toBeTruthy();
    await expect(client.call('version')).rejects.toMatchObject({ code: 'core-unavailable' });
    expect(spawnFn).toHaveBeenCalledTimes(2);
  });

  it('spawns detached, hidden, with its own cwd and without ELECTRON_RUN_AS_NODE', async () => {
    process.env.ELECTRON_RUN_AS_NODE = '1';
    const procs: FakeProc[] = [];
    const spawnFn: any = vi.fn(() => {
      const p = new FakeProc();
      procs.push(p);
      return p;
    });
    const client = new CoreClient('/bin/gitglance', spawnFn, '/ext');
    void client.call('version').catch(() => {});
    await tick();
    delete process.env.ELECTRON_RUN_AS_NODE;
    const opts = spawnFn.mock.calls[0][2];
    expect(opts).toMatchObject({ detached: true, windowsHide: true, cwd: '/ext' });
    expect(opts.stdio).toEqual(['pipe', 'pipe', 'pipe']);
    expect(opts.env.ELECTRON_RUN_AS_NODE).toBeUndefined();
  });

  it('ignores a late line from a process that is no longer current', async () => {
    const { client, procs } = setup();
    const first = client.call('view').catch((e) => e);
    await tick();
    const oldId = procs[0].written[0].id;
    procs[0].exit(1);
    await first;
    const second = client.call('view');
    await tick();
    expect(procs[1].written[0].id).not.toBe(oldId); // ids keep counting
    // the dead process speaks late, using the id of the live call
    procs[0].reply({ id: procs[1].written[0].id, result: 'stale' });
    await tick();
    procs[1].reply({ id: procs[1].written[0].id, result: 'fresh' });
    expect(await second).toBe('fresh');
  });

  it('counts a spawn error as a failed start', async () => {
    const spawnFn: any = vi.fn(() => {
      const p = new FakeProc();
      queueMicrotask(() => p.emit('error', new Error('ENOENT')));
      return p;
    });
    const client = new CoreClient('/nope', spawnFn);
    await expect(client.call('version')).rejects.toBeTruthy();
    await expect(client.call('version')).rejects.toBeTruthy();
    await expect(client.call('version')).rejects.toMatchObject({ code: 'core-unavailable' });
    expect(spawnFn).toHaveBeenCalledTimes(2);
  });

  it('starts afresh after allowRestart, once it had given up', async () => {
    const { client, procs, spawnFn } = setup();
    for (let i = 0; i < 2; i++) {
      const c = client.call('version');
      await tick();
      procs[i].exit(1);
      await expect(c).rejects.toBeTruthy();
    }
    await expect(client.call('version')).rejects.toMatchObject({ code: 'core-unavailable' });
    client.allowRestart();
    const next = client.call('version');
    await tick();
    expect(spawnFn).toHaveBeenCalledTimes(3);
    procs[2].reply({ id: procs[2].written[0].id, result: 'back' });
    expect(await next).toBe('back');
  });

  it('marks the core executable before starting it outside Windows', async () => {
    const order: string[] = [];
    const chmod = vi.fn((p: string, mode: number) => void order.push(`chmod ${p} ${mode.toString(8)}`));
    const spawnFn: any = vi.fn(() => {
      order.push('spawn');
      return new FakeProc();
    });
    const client = new CoreClient('/ext/bin/gitglance', spawnFn, undefined, { platform: 'linux', chmod });
    void client.call('version').catch(() => undefined);
    await tick();
    expect(order).toEqual(['chmod /ext/bin/gitglance 755', 'spawn']);
  });

  it('does not chmod on Windows', async () => {
    const chmod = vi.fn();
    const spawnFn: any = vi.fn(() => new FakeProc());
    const client = new CoreClient('C:/ext/bin/gitglance.exe', spawnFn, undefined, { platform: 'win32', chmod });
    void client.call('version').catch(() => undefined);
    await tick();
    expect(chmod).not.toHaveBeenCalled();
    expect(spawnFn).toHaveBeenCalledTimes(1);
  });

  it('still tries to start when chmod fails and names the chmod error when the start fails', async () => {
    const chmod = vi.fn(() => {
      throw new Error('EROFS: read-only file system');
    });
    const spawnFn: any = vi.fn(() => {
      const p = new FakeProc();
      queueMicrotask(() => p.emit('error', new Error('EACCES')));
      return p;
    });
    const lines: string[] = [];
    const client = new CoreClient('/ext/bin/gitglance', spawnFn, undefined, { platform: 'darwin', chmod });
    client.onLog((l) => lines.push(l));
    await expect(client.call('version')).rejects.toBeTruthy();
    await expect(client.call('version')).rejects.toBeTruthy();
    const e = await client.call('version').catch((x) => x);
    expect(spawnFn).toHaveBeenCalledTimes(2);
    expect(e).toMatchObject({ code: 'core-unavailable' });
    expect(e.message).toContain('EROFS');
    expect(lines.join('\n')).toMatch(/executable.*EROFS/);
  });
});

describe('write methods', () => {
  it('match the write set of the core method table', () => {
    const src = fs.readFileSync(path.resolve(__dirname, '../../core/internal/rpc/methods.go'), 'utf8');
    const table = src.slice(src.indexOf('func (h *Handler) table()'));
    const body = table.slice(0, table.indexOf('\n}\n'));
    const entries = [...body.matchAll(/"(\w+)":\s*(read|write)\(/g)].map((m) => [m[1], m[2]] as const);
    expect(entries.length).toBeGreaterThan(20);
    const writes = entries.filter(([, kind]) => kind === 'write').map(([name]) => name).sort();
    expect([...WRITE_METHODS].sort()).toEqual(writes);
  });
});

describe('CoreClient timeouts and dispose', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval'] });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('rejects a timed-out call without killing the process', async () => {
    const { client, procs } = setup();
    const caught = client.call('view', {}, 1000).catch((e) => e);
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(1001);
    expect(await caught).toMatchObject({ code: 'timeout' });
    expect(procs[0].killed).toBe(false);
    // a late reply is ignored and the process still serves new calls
    procs[0].reply({ id: procs[0].written[0].id, result: 'late' });
    const q = client.call('version');
    await vi.advanceTimersByTimeAsync(0);
    procs[0].reply({ id: procs[0].written[1].id, result: 'fine' });
    expect(await q).toBe('fine');
  });

  const MIN = 60_000;
  // limitOf measures after how long a call of `method` gives up.
  const limitOf = async (method: string, within: number[]) => {
    const { client } = setup();
    const p = client.call(method, { repo: 'r' }).catch((e) => e);
    let settled = false;
    void p.then(() => (settled = true));
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(within[0] - 1000);
    const before = settled;
    await vi.advanceTimersByTimeAsync(within[1] - within[0] + 2000);
    return { before, error: await p };
  };

  for (const [method, ms, code] of [
    ['align', 20 * MIN, 'still-running'],
    ['fetch', 20 * MIN, 'still-running'],
    ['removeFolder', 30 * MIN, 'still-running'],
    ['createFolder', 30 * MIN, 'still-running'],
    ['switch', 30 * MIN, 'still-running'],
    ['view', 5 * MIN, 'timeout'],
    ['compare', 5 * MIN, 'timeout'],
    ['repos', 60_000, 'timeout'],
    ['ops', 60_000, 'timeout'],
  ] as const) {
    it(`gives ${method} ${ms / 1000} s and then answers ${code}`, async () => {
      const { before, error } = await limitOf(method, [ms, ms]);
      expect(before).toBe(false);
      expect(error).toMatchObject({ code });
    });
  }

  it('says a timed-out write is still running and logs its late answer', async () => {
    const { client, procs } = setup();
    const lines: string[] = [];
    client.onLog((l) => lines.push(l));
    const p = client.call('removeFolder', { repo: 'r' }).catch((e) => e);
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(30 * MIN + 1);
    const e = await p;
    expect(e).toMatchObject({ code: 'still-running' });
    expect(e.message).toMatch(/operation log/);
    procs[0].reply({ id: procs[0].written[0].id, result: { ok: true } });
    await vi.advanceTimersByTimeAsync(0);
    expect(lines.join('\n')).toMatch(/removeFolder.*finished after its time limit/);
    expect(lines.join('\n')).toContain('"ok":true');
  });

  it('localizes its error messages', async () => {
    l10n.calls.length = 0;
    const { client } = setup();
    const p = client.call('view', {}, 1000).catch((e) => e);
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(1001);
    expect((await p).message).toBe('view did not answer within 1 s');
    expect(l10n.calls).toContain('{0} did not answer within {1} s');
  });

  it('dispose closes stdin and never kills while a write is pending', async () => {
    const { client, procs } = setup();
    const outcome = client.call('align', { repo: 'r' }).then(
      (v) => v,
      (e) => e,
    );
    await vi.advanceTimersByTimeAsync(0);
    const done = client.dispose();
    await vi.advanceTimersByTimeAsync(0);
    expect(procs[0].stdinEnded).toBe(true);
    await vi.advanceTimersByTimeAsync(30_000);
    expect(procs[0].killed).toBe(false);
    // the write finishes, then the core exits by itself
    procs[0].reply({ id: procs[0].written[0].id, result: 'done' });
    expect(await outcome).toBe('done');
    procs[0].exit(0);
    await done;
    expect(procs[0].killed).toBe(false);
  });

  it('dispose kills an idle process only after the grace period', async () => {
    const { client, procs } = setup();
    const p = client.call('version');
    await vi.advanceTimersByTimeAsync(0);
    procs[0].reply({ id: procs[0].written[0].id, result: 1 });
    await p;
    const done = client.dispose();
    await vi.advanceTimersByTimeAsync(2900);
    expect(procs[0].killed).toBe(false);
    await vi.advanceTimersByTimeAsync(200);
    expect(procs[0].killed).toBe(true);
    await done;
  });

  it('dispose resolves at once when the core exits on its own', async () => {
    const { client, procs } = setup();
    const p = client.call('version');
    await vi.advanceTimersByTimeAsync(0);
    procs[0].reply({ id: procs[0].written[0].id, result: 1 });
    await p;
    const done = client.dispose();
    procs[0].exit(0);
    await done;
    expect(procs[0].killed).toBe(false);
  });

  it('rejects calls after dispose', async () => {
    const { client, spawnFn } = setup();
    await client.dispose();
    await expect(client.call('version')).rejects.toMatchObject({ code: 'core-disposed' });
    expect(spawnFn).not.toHaveBeenCalled();
  });
});
