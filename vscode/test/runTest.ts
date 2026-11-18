import { execFileSync } from 'node:child_process';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { runTests } from '@vscode/test-electron';

// Runs the smoke suite in a VS Code that test-electron downloads into
// .vscode-test, with its own user data and extensions folders. It opens a
// throw-away repository, never a real one.
async function main() {
  // Start from a clean environment: nothing of the host editor's VSCODE_* or
  // ELECTRON_* settings may reach the test instance.
  for (const key of Object.keys(process.env)) {
    if (key.startsWith('VSCODE_') || key.startsWith('ELECTRON_')) delete process.env[key];
  }
  const root = path.resolve(__dirname, '..');
  const sandbox = path.join(root, '.vscode-test');
  fs.mkdirSync(sandbox, { recursive: true });
  const repo = fs.mkdtempSync(path.join(os.tmpdir(), 'gitglance-smoke-'));
  const git = (...args: string[]) => execFileSync('git', ['-C', repo, ...args], { stdio: 'ignore' });
  git('init', '-q', '-b', 'main');
  fs.writeFileSync(path.join(repo, 'a.txt'), 'a\n');
  git('add', 'a.txt');
  git('-c', 'user.name=t', '-c', 'user.email=t@example.com', 'commit', '-q', '-m', 'init');
  const home = fs.mkdtempSync(path.join(os.tmpdir(), 'gitglance-home-'));
  try {
    await runTests({
      cachePath: path.join(sandbox, 'vscode'),
      extensionDevelopmentPath: root,
      extensionTestsPath: path.join(root, 'dist-test', 'suite', 'index.js'),
      extensionTestsEnv: { GITGLANCE_HOME: home },
      launchArgs: [
        repo,
        '--user-data-dir',
        path.join(sandbox, 'user-data'),
        '--extensions-dir',
        path.join(sandbox, 'extensions'),
        '--disable-extensions',
        '--disable-workspace-trust',
      ],
    });
  } finally {
    fs.rmSync(repo, { recursive: true, force: true });
    fs.rmSync(home, { recursive: true, force: true });
  }
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
