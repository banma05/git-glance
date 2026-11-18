import * as assert from 'node:assert';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import * as vscode from 'vscode';

type Traffic = Array<{ mode: string; dir: string; message: any }>;

const wait = (ms: number) => new Promise((r) => setTimeout(r, ms));
const norm = (p: string) => p.replace(/\\/g, '/').replace(/\/+$/, '').toLowerCase();

// until polls fn every 100 ms until it returns a value, or fails after ms.
async function until<T>(what: string, ms: number, fn: () => T | undefined): Promise<T> {
  const end = Date.now() + ms;
  for (;;) {
    const v = fn();
    if (v !== undefined) return v;
    if (Date.now() > end) throw new Error(`timed out waiting for ${what}`);
    await wait(100);
  }
}

// reposAnswers lists what the sidebar page was told for each "repos" call, in order.
function reposAnswers(t: Traffic): unknown[] {
  const asked = new Set(t.filter((e) => e.mode === 'sidebar' && e.dir === 'in' && e.message?.method === 'repos').map((e) => e.message.id));
  return t.filter((e) => e.mode === 'sidebar' && e.dir === 'out' && e.message?.type === 'result' && asked.has(e.message.id)).map((e) => e.message.result ?? e.message.error);
}

suite('Git Glance smoke', () => {
  test('runs with its own config folder, and the sidebar page gets the workspace repository', async function () {
    this.timeout(90_000);
    const home = process.env.GITGLANCE_HOME ?? '';
    assert.ok(home, 'GITGLANCE_HOME is set for the test host');
    assert.ok(norm(home).startsWith(norm(os.tmpdir())), `GITGLANCE_HOME ${home} is a temporary folder`);
    for (const base of [process.env.APPDATA, path.join(os.homedir(), '.config'), path.join(os.homedir(), 'Library', 'Application Support')]) {
      if (base) assert.notStrictEqual(norm(home), norm(path.join(base, 'git-glance')), 'not the real config folder');
    }
    const workspace = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath ?? '';
    assert.ok(workspace, 'a workspace folder is open');

    const ext = vscode.extensions.getExtension('banma05.git-glance');
    assert.ok(ext, 'extension is installed in the test host');
    // Opening the view activates the extension, as it does for a user, so the
    // page loads while startup discovery is still running.
    await vscode.commands.executeCommand('workbench.view.extension.gitglance');
    await until('activation', 20_000, () => (ext.isActive ? true : undefined));
    const api = ext.exports;

    const first = await until('the sidebar page to read the repositories', 30_000, () => reposAnswers(api.traffic())[0]);
    assert.ok(Array.isArray(first), `repos answered with a list: ${JSON.stringify(first)}`);
    assert.deepStrictEqual(
      (first as string[]).map(norm),
      [norm(workspace)],
      'the first list the sidebar page gets already holds the workspace repository',
    );

    // The core wrote its list into GITGLANCE_HOME, nowhere else.
    const global = JSON.parse(fs.readFileSync(path.join(home, 'global.json'), 'utf8'));
    assert.deepStrictEqual(global.repos.map(norm), [norm(workspace)]);

    const v = await api.core().call('version');
    assert.strictEqual(v.gitOK, true);

    // Refresh renders the page again; the new page is served and gets the list.
    const before = reposAnswers(api.traffic()).length;
    await vscode.commands.executeCommand('gitglance.refresh');
    const again = await until('the refreshed page to read the repositories', 30_000, () => reposAnswers(api.traffic())[before]);
    assert.deepStrictEqual((again as string[]).map(norm), [norm(workspace)]);

    // Every list came with what discovery could not add (nothing here), and
    // every page message carried its render token and its instance id.
    const t = api.traffic() as Traffic;
    const told = t.filter((e) => e.mode === 'sidebar' && e.dir === 'out' && e.message?.type === 'discovery');
    assert.ok(told.length >= 2, 'discovery problems are sent with each repository list');
    assert.ok(told.every((e) => Array.isArray(e.message.problems) && e.message.problems.length === 0));
    const asked = t.filter((e) => e.mode === 'sidebar' && e.dir === 'in');
    assert.ok(asked.every((e) => typeof e.message.page === 'string' && /^[0-9a-f]{32}$/.test(e.message.instance)));
  });
});
