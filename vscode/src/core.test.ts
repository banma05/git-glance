import { EventEmitter } from 'node:events';
import { PassThrough } from 'node:stream';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { CoreClient } from './core';

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
    this.emit('exit', code, null);
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

  it('uses 180 s for align and fetch', async () => {
    const { client } = setup();
    const p = client.call('align', { repo: 'r' }).catch((e) => e);
    let settled = false;
    p.then(() => (settled = true));
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(179_000);
    expect(settled).toBe(false);
    await vi.advanceTimersByTimeAsync(2000);
    expect(await p).toMatchObject({ code: 'timeout' });
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
