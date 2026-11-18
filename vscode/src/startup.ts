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
    if (!this.finished) {
      let timer: ReturnType<typeof setTimeout> | undefined;
      const limit = new Promise<void>((resolve) => (timer = setTimeout(resolve, this.limitMs)));
      await Promise.race([this.settled, limit]);
      clearTimeout(timer);
    }
    if (this.finished) return this.problems;
    this.log(`held the repository list for ${this.limitMs} ms; discovery is still running`);
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
