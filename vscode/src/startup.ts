// Discovery is the startup registration of the workspace repositories. A page
// that asks for the repository list while it runs waits for it, for limitMs at
// most; a page that was answered before it ended is told once, when it ends,
// to read the list again. Without this a page that loads quickly reads the list
// before the workspace repository is in it and keeps showing "no repository".
export class Discovery {
  private finished = false;
  private early = new Map<unknown, () => void>();
  private readonly settled: Promise<void>;

  constructor(
    work: Promise<unknown>,
    private readonly limitMs: number,
  ) {
    this.settled = work.then(
      () => this.finish(),
      () => this.finish(),
    );
  }

  get done(): boolean {
    return this.finished;
  }

  // beforeRepos returns when the page may read the repository list. page names
  // the page; notify tells it to read again and is kept only when the page is
  // answered before discovery ends.
  async beforeRepos(page: unknown, notify: () => void): Promise<void> {
    if (!this.finished) {
      let timer: ReturnType<typeof setTimeout> | undefined;
      const limit = new Promise<void>((resolve) => (timer = setTimeout(resolve, this.limitMs)));
      await Promise.race([this.settled, limit]);
      clearTimeout(timer);
    }
    if (!this.finished) this.early.set(page, notify);
  }

  private finish(): void {
    this.finished = true;
    const pages = [...this.early.values()];
    this.early.clear();
    pages.forEach((notify) => notify());
  }
}
