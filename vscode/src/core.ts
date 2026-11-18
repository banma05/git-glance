import { spawn as nodeSpawn } from 'node:child_process';
import type { ChildProcess } from 'node:child_process';

export class CoreError extends Error {
  code: string;
  constructor(code: string, message: string) {
    super(message);
    this.name = 'CoreError';
    this.code = code;
  }
}

// Core methods that change the machine. dispose() never kills the core while
// one of these is still running.
const WRITE_METHODS = new Set([
  'align',
  'switch',
  'createFolder',
  'removeFolder',
  'setRole',
  'moveTrunk',
  'assign',
  'setRepoSettings',
  'setGlobal',
  'addRepo',
  'removeRepo',
  'fetch',
  'clearBackups',
]);

const DEFAULT_TIMEOUT = 60_000;
const LONG_TIMEOUT = 180_000;
const LONG_METHODS = new Set(['align', 'fetch']);
const MAX_FAILED_STARTS = 2;
const KILL_GRACE_MS = 3000;

type SpawnFn = typeof nodeSpawn;

interface Pending {
  resolve: (v: any) => void;
  reject: (e: Error) => void;
  timer: ReturnType<typeof setTimeout> | undefined;
  write: boolean;
  settled: boolean; // rejected already (timeout); the entry stays while a write may still run
}

// CoreClient talks JSON lines to "<bin> rpc". Replies are matched by id and may
// arrive in any order.
export class CoreClient {
  private proc: ChildProcess | null = null;
  private pending = new Map<number, Pending>();
  private nextId = 1;
  private failedStarts = 0;
  private disposed = false;
  private exitWaiters: Array<() => void> = [];
  private log: (line: string) => void = () => {};

  constructor(
    private readonly binPath: string,
    private readonly spawnFn: SpawnFn = nodeSpawn,
    private readonly cwd?: string,
  ) {}

  onLog(fn: (line: string) => void): void {
    this.log = fn;
  }

  call<T = any>(method: string, params?: object, timeoutMs?: number): Promise<T> {
    if (this.disposed) return Promise.reject(new CoreError('core-disposed', 'the core client was shut down'));
    const proc = this.ensureProcess();
    if (!proc) return Promise.reject(new CoreError('core-unavailable', 'the core program could not be started'));
    const id = this.nextId++;
    const limit = timeoutMs ?? (LONG_METHODS.has(method) ? LONG_TIMEOUT : DEFAULT_TIMEOUT);
    return new Promise<T>((resolve, reject) => {
      const entry: Pending = { resolve, reject, timer: undefined, write: WRITE_METHODS.has(method), settled: false };
      entry.timer = setTimeout(() => {
        // Reject the caller but keep the process: the core may still be working.
        entry.settled = true;
        entry.timer = undefined;
        reject(new CoreError('timeout', `${method} did not answer within ${Math.round(limit / 1000)} s`));
        if (!entry.write) this.pending.delete(id);
      }, limit);
      this.pending.set(id, entry);
      const line = JSON.stringify({ id, method, params: params ?? {} }) + '\n';
      try {
        proc.stdin!.write(line);
      } catch (e) {
        this.finish(id);
        reject(new CoreError('core-exited', String(e)));
      }
    });
  }

  // dispose closes the core's input and waits for it to finish. It kills the
  // process only when no write is running and the grace period has passed.
  dispose(): Promise<void> {
    this.disposed = true;
    const proc = this.proc;
    if (!proc) return Promise.resolve();
    // Waiting reads are of no use any more; writes are left to finish.
    for (const [id, p] of [...this.pending]) {
      if (!p.write && !p.settled) {
        this.finish(id);
        p.reject(new CoreError('core-disposed', 'the core client was shut down'));
      }
    }
    return new Promise<void>((resolve) => {
      this.exitWaiters.push(resolve);
      try {
        proc.stdin!.end();
      } catch {
        // already closed
      }
      const check = () => {
        if (this.proc !== proc) return;
        if (this.writesInFlight() > 0) {
          setTimeout(check, KILL_GRACE_MS);
          return;
        }
        proc.kill();
      };
      setTimeout(check, KILL_GRACE_MS);
    });
  }

  private writesInFlight(): number {
    let n = 0;
    for (const p of this.pending.values()) if (p.write) n++;
    return n;
  }

  private finish(id: number): Pending | undefined {
    const p = this.pending.get(id);
    if (!p) return undefined;
    this.pending.delete(id);
    if (p.timer) clearTimeout(p.timer);
    return p;
  }

  private ensureProcess(): ChildProcess | null {
    if (this.proc) return this.proc;
    if (this.failedStarts >= MAX_FAILED_STARTS) return null;
    let proc: ChildProcess;
    try {
      // detached: on Windows a non-detached child is tied to the parent's job
      // object and is killed when the extension host exits, even mid-write.
      // The core ends by itself once stdin closes and its running work is done.
      const env = { ...process.env };
      delete env.ELECTRON_RUN_AS_NODE;
      proc = this.spawnFn(this.binPath, ['rpc'], {
        stdio: ['pipe', 'pipe', 'pipe'],
        detached: true,
        windowsHide: true,
        cwd: this.cwd,
        env,
      });
    } catch (e) {
      this.failedStarts++;
      this.log(`core start failed: ${e}`);
      return this.ensureProcess();
    }
    this.proc = proc;
    let answered = false;
    let buf = '';
    proc.stdout!.setEncoding('utf8');
    proc.stdout!.on('data', (chunk: string) => {
      buf += chunk;
      let i: number;
      while ((i = buf.indexOf('\n')) >= 0) {
        const line = buf.slice(0, i);
        buf = buf.slice(i + 1);
        if (this.proc === proc && this.onLine(line)) {
          answered = true;
          this.failedStarts = 0;
        }
      }
    });
    proc.stderr?.setEncoding('utf8');
    proc.stderr?.on('data', (s: string) => this.log(`core: ${s.trimEnd()}`));
    proc.stdin!.on('error', () => {
      // a broken pipe also shows up as an exit event
    });
    const gone = (why: string) => {
      if (this.proc !== proc) return;
      this.proc = null;
      if (!answered) this.failedStarts++;
      this.log(`core exited: ${why}`);
      for (const id of [...this.pending.keys()]) {
        const p = this.finish(id)!;
        if (!p.settled) p.reject(new CoreError('core-exited', `the core program stopped (${why})`));
      }
      const waiters = this.exitWaiters;
      this.exitWaiters = [];
      waiters.forEach((w) => w());
    };
    proc.on('error', (e) => gone(String(e)));
    proc.on('close', (code, signal) => gone(`code ${code} signal ${signal}`));
    return proc;
  }

  // onLine handles one reply line; true when it answered a request.
  private onLine(line: string): boolean {
    if (!line.trim()) return false;
    let msg: any;
    try {
      msg = JSON.parse(line);
    } catch {
      this.log(`core sent a line that is not JSON`);
      return false;
    }
    if (!msg || typeof msg.id !== 'number') return false;
    const p = this.finish(msg.id);
    if (!p) return true; // late answer to a call that already timed out
    if (p.settled) return true;
    if (msg.error) p.reject(new CoreError(String(msg.error.code ?? 'error'), String(msg.error.message ?? '')));
    else p.resolve(msg.result);
    return true;
  }
}
