import { spawn as nodeSpawn } from 'node:child_process';
import type { ChildProcess } from 'node:child_process';
import { chmodSync } from 'node:fs';
import * as vscode from 'vscode';

export class CoreError extends Error {
  code: string;
  constructor(code: string, message: string) {
    super(message);
    this.name = 'CoreError';
    this.code = code;
  }
}

// Core methods that change the machine: the "write" entries of the method table
// in core/internal/rpc/methods.go (a test compares the two). dispose() never
// kills the core while one of these is still running.
export const WRITE_METHODS: ReadonlySet<string> = new Set([
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

const MINUTE = 60_000;
const READ_TIMEOUT = MINUTE;
const LONG_READ_TIMEOUT = 5 * MINUTE; // view and compare walk the history
const WRITE_TIMEOUT = 30 * MINUTE;
const NETWORK_TIMEOUT = 20 * MINUTE; // fetch and align read the remote
const LONG_READS = new Set(['view', 'compare']);
const NETWORK_WRITES = new Set(['fetch', 'align']);
const MAX_FAILED_STARTS = 2;
const KILL_GRACE_MS = 3000;

// timeoutFor is how long a call of method may take before its caller gives up.
// The core keeps running a write that took longer; only the wait ends.
export function timeoutFor(method: string): number {
  if (NETWORK_WRITES.has(method)) return NETWORK_TIMEOUT;
  if (WRITE_METHODS.has(method)) return WRITE_TIMEOUT;
  return LONG_READS.has(method) ? LONG_READ_TIMEOUT : READ_TIMEOUT;
}

type SpawnFn = typeof nodeSpawn;

export interface CoreOptions {
  platform?: string; // process.platform by default
  chmod?: (path: string, mode: number) => void; // makes the packaged core executable
}

interface Pending {
  method: string;
  resolve: (v: any) => void;
  reject: (e: Error) => void;
  timer: ReturnType<typeof setTimeout> | undefined;
  write: boolean;
  settled: boolean; // rejected already (timeout); the entry stays while a write may still run
  stopListening?: () => void; // removes the abort listener of the caller's signal
}

// CoreClient talks JSON lines to "<bin> rpc". Replies are matched by id and may
// arrive in any order.
export class CoreClient {
  private proc: ChildProcess | null = null;
  private pending = new Map<number, Pending>();
  private nextId = 1;
  private failedStarts = 0;
  private startProblem = ''; // why the last start failed, for the core-unavailable message
  private disposed = false;
  private exitWaiters: Array<() => void> = [];
  private lateReads = new Map<number, { method: string; at: number }>(); // reads that timed out, by id
  private quiet = new Set<number>(); // ids whose answer is of no interest: cancels, and reads cancelled by their caller
  private log: (line: string) => void = () => {};
  private readonly platform: string;
  private readonly chmod: (path: string, mode: number) => void;

  constructor(
    private readonly binPath: string,
    private readonly spawnFn: SpawnFn = nodeSpawn,
    private readonly cwd?: string,
    opts: CoreOptions = {},
  ) {
    this.platform = opts.platform ?? process.platform;
    this.chmod = opts.chmod ?? chmodSync;
  }

  onLog(fn: (line: string) => void): void {
    this.log = fn;
  }

  // allowRestart lets the next call start the core again after it gave up
  // (core-unavailable), for example when the user asks for a refresh.
  allowRestart(): void {
    this.failedStarts = 0;
  }

  // call runs a core method. signal lets the caller give up on a read: the
  // core is asked to cancel it and the call rejects with code cancelled. A
  // write is never cancelled; it runs to its end whatever the caller does.
  call<T = any>(method: string, params?: object, timeoutMs?: number, signal?: AbortSignal): Promise<T> {
    if (this.disposed) return Promise.reject(new CoreError('core-disposed', vscode.l10n.t('The core client was shut down.')));
    const write = WRITE_METHODS.has(method);
    if (!write && signal?.aborted) return Promise.reject(this.cancelled());
    const proc = this.ensureProcess();
    if (!proc) return Promise.reject(this.unavailable());
    const id = this.nextId++;
    const limit = timeoutMs ?? timeoutFor(method);
    return new Promise<T>((resolve, reject) => {
      const entry: Pending = { method, resolve, reject, timer: undefined, write, settled: false };
      entry.timer = setTimeout(() => {
        // Reject the caller but keep the process: the core may still be working.
        entry.settled = true;
        entry.timer = undefined;
        if (write) {
          reject(this.stillRunning(method, limit));
          return;
        }
        this.finish(id);
        this.log(`${method} timed out after ${limit} ms`);
        this.lateReads.set(id, { method, at: Date.now() });
        this.sendCancel(id);
        reject(this.timedOut(method, limit));
      }, limit);
      if (!write && signal) {
        const onAbort = () => {
          if (!this.finish(id)) return;
          this.quiet.add(id);
          this.sendCancel(id);
          reject(this.cancelled());
        };
        signal.addEventListener('abort', onAbort, { once: true });
        entry.stopListening = () => signal.removeEventListener('abort', onAbort);
      }
      this.pending.set(id, entry);
      const line = JSON.stringify({ id, method, params: params ?? {} }) + '\n';
      try {
        proc.stdin!.write(line);
      } catch (e) {
        this.finish(id);
        reject(new CoreError('core-exited', vscode.l10n.t('The core program stopped ({0}).', String(e))));
      }
    });
  }

  // sendCancel asks the core to stop the read with id target; its answer is ignored.
  private sendCancel(target: number): void {
    const proc = this.proc;
    if (!proc) return;
    const id = this.nextId++;
    this.quiet.add(id);
    try {
      proc.stdin!.write(JSON.stringify({ id, method: 'cancel', params: { target } }) + '\n');
    } catch {
      this.quiet.delete(id);
    }
  }

  private cancelled(): CoreError {
    return new CoreError('cancelled', vscode.l10n.t('The call was cancelled.'));
  }

  private timedOut(method: string, ms: number): CoreError {
    return new CoreError('timeout', vscode.l10n.t('{0} did not answer within {1} s', method, Math.round(ms / 1000)));
  }

  private stillRunning(method: string, ms: number): CoreError {
    return new CoreError(
      'still-running',
      vscode.l10n.t('{0} is still running after {1} min; when it ends, its answer is written to the Git Glance output', method, Math.round(ms / MINUTE)),
    );
  }

  private unavailable(): CoreError {
    const why = this.startProblem || vscode.l10n.t('it stopped right after starting');
    return new CoreError('core-unavailable', vscode.l10n.t('The core program could not be started: {0}', why));
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
        p.reject(new CoreError('core-disposed', vscode.l10n.t('The core client was shut down.')));
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
    p.stopListening?.();
    return p;
  }

  // makeExecutable sets the executable bits of the packaged core outside
  // Windows: a VSIX does not keep file modes. A failure is logged and kept for
  // the error message; starting is still tried, the bits may be set already.
  private makeExecutable(): void {
    if (this.platform === 'win32') return;
    try {
      this.chmod(this.binPath, 0o755);
    } catch (e) {
      this.startProblem = vscode.l10n.t('cannot make {0} executable: {1}', this.binPath, String(e));
      this.log(`cannot make the core executable (${this.binPath}): ${e}`);
    }
  }

  private ensureProcess(): ChildProcess | null {
    if (this.proc) return this.proc;
    if (this.failedStarts >= MAX_FAILED_STARTS) return null;
    this.startProblem = '';
    this.makeExecutable();
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
      this.noteStartProblem(String(e));
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
      if (!answered) {
        this.failedStarts++;
        this.noteStartProblem(why);
      }
      this.log(`core exited: ${why}`);
      this.lateReads.clear();
      this.quiet.clear();
      for (const id of [...this.pending.keys()]) {
        const p = this.finish(id)!;
        if (!p.settled) p.reject(new CoreError('core-exited', vscode.l10n.t('The core program stopped ({0}).', why)));
      }
      const waiters = this.exitWaiters;
      this.exitWaiters = [];
      waiters.forEach((w) => w());
    };
    proc.on('error', (e) => gone(String(e)));
    proc.on('close', (code, signal) => gone(`code ${code} signal ${signal}`));
    return proc;
  }

  // noteStartProblem keeps the first reason a start failed (a chmod failure
  // explains a later EACCES better than the EACCES itself).
  private noteStartProblem(why: string): void {
    if (!this.startProblem) this.startProblem = why;
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
    if (this.quiet.delete(msg.id)) return true;
    const late = this.lateReads.get(msg.id);
    if (late) {
      // Only how it ended: a read's result may hold file contents.
      this.lateReads.delete(msg.id);
      const outcome = msg.error ? String(msg.error.code ?? 'error') : 'ok';
      this.log(`${late.method} answered ${Date.now() - late.at} ms after its time limit: ${outcome}`);
      return true;
    }
    const p = this.finish(msg.id);
    if (!p) return true; // an answer nobody waits for any more
    if (p.settled) {
      // A write that outlived its wait: its caller was told it is still
      // running, so the answer goes to the log (the op log has it too).
      const outcome = msg.error ? `error ${JSON.stringify(msg.error)}` : JSON.stringify(msg.result);
      this.log(`${p.method} finished after its time limit: ${outcome}`);
      return true;
    }
    if (msg.error) p.reject(new CoreError(String(msg.error.code ?? 'error'), String(msg.error.message ?? '')));
    else p.resolve(msg.result);
    return true;
  }
}
