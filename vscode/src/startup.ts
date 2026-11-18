// A workspace folder startup discovery could not add, and why.
export interface DiscoveryProblem {
  folder: string; // "" when the whole run failed
  code: string;
  message: string;
}

// What one run of startup discovery ended with.
export interface DiscoveryOutcome {
  repos: number; // repositories in the list afterwards; -1 when unknown
  problems: DiscoveryProblem[];
}

// Discovery is the registration of the workspace repositories, run at
// activation and again on "Git Glance: Refresh". A page that asks for the
// repository list while it runs waits for it, for limitMs at most; a page that
// was answered before it ended is told once, when it ends, to read the list
// again. Without this a page that loads quickly reads the list before the
// workspace repository is in it and keeps showing "no repository".
export class Discovery {
  private finished = true;
  private runs = 0;
  private settled: Promise<void> = Promise.resolve();
  private problems: DiscoveryProblem[] = [];
  private early = new Map<unknown, () => void>();

  constructor(
    private readonly limitMs: number,
    private readonly log: (line: string) => void,
  ) {}

  get done(): boolean {
    return this.finished;
  }

  // run starts a run; it replaces one still running, whose end is then ignored.
  run(work: () => Promise<DiscoveryOutcome>): void {
    const mine = ++this.runs;
    const started = Date.now();
    this.finished = false;
    this.settled = work().then(
      (outcome) => this.finish(mine, started, outcome),
      (e: any) =>
        this.finish(mine, started, { repos: -1, problems: [{ folder: '', code: String(e?.code ?? 'error'), message: String(e?.message ?? e) }] }),
    );
  }

  // beforeRepos returns when the page may read the repository list, with the
  // problems of the last finished run. page names the page; notify tells it to
  // read again and is kept only when the page is answered before the run ends.
  async beforeRepos(page: unknown, notify: () => void): Promise<DiscoveryProblem[]> {
    const started = Date.now();
    if (!this.finished) {
      let timer: ReturnType<typeof setTimeout> | undefined;
      const limit = new Promise<void>((resolve) => (timer = setTimeout(resolve, this.limitMs)));
      await Promise.race([this.settled, limit]);
      clearTimeout(timer);
    }
    if (this.finished) return this.problems;
    // the limit, or a run that was replaced meanwhile, ended the wait
    this.log(`held the repository list for ${Date.now() - started} ms; discovery is still running`);
    this.early.set(page, notify);
    return [];
  }

  private finish(run: number, started: number, outcome: DiscoveryOutcome): void {
    if (run !== this.runs) return;
    this.finished = true;
    this.problems = outcome.problems;
    this.log(`discovery done: ${outcome.repos} repositories listed, ${outcome.problems.length} folders skipped, ${Date.now() - started} ms`);
    const pages = [...this.early.values()];
    this.early.clear();
    pages.forEach((notify) => notify());
  }
}

// registerFolders adds each folder that is inside a repository to the list the
// pages show. A folder that is no repository is only logged; any other failure
// is logged and reported, so the pages can say why. Never throws.
export async function registerFolders(
  core: { call<T>(method: string, params?: object): Promise<T> },
  folders: string[],
  log: (line: string) => void,
): Promise<DiscoveryOutcome> {
  const problems: DiscoveryProblem[] = [];
  let repos = -1;
  for (const folder of folders) {
    try {
      const found = await core.call<{ repo: string }>('discover', { path: folder });
      repos = (await core.call<string[]>('addRepo', { path: found.repo })).length;
    } catch (e: any) {
      const code = String(e?.code ?? 'error');
      const message = String(e?.message ?? e);
      log(`skipping ${folder}: ${code}: ${message}`);
      if (code !== 'not-repo') problems.push({ folder, code, message });
    }
  }
  return { repos, problems };
}
