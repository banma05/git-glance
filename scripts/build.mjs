// Build script: node scripts/build.mjs <ui|core|vsix|all> [--targets host|all]
// Runs without a shell. Cleans only its own outputs: dist/, vscode/media/ui,
// vscode/bin/<target>, and core/internal/web/dist (except .keep).
import { spawnSync } from 'node:child_process';
import {
  cpSync, existsSync, mkdirSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync,
} from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const p = (...parts) => join(root, ...parts);

const GOOS_TO_PLATFORM = { windows: 'win32', darwin: 'darwin', linux: 'linux' };
const GOARCH_TO_ARCH = { amd64: 'x64', arm64: 'arm64' };
const ALL_TARGETS = ['windows', 'darwin', 'linux'].flatMap((goos) =>
  ['amd64', 'arm64'].map((goarch) => ({ goos, goarch })));

function hostTarget() {
  const goos = { win32: 'windows', darwin: 'darwin', linux: 'linux' }[process.platform];
  const goarch = { x64: 'amd64', arm64: 'arm64' }[process.arch];
  if (!goos || !goarch) throw new Error(`unsupported host ${process.platform}/${process.arch}`);
  return { goos, goarch };
}

const label = (t) => `${GOOS_TO_PLATFORM[t.goos]}-${GOARCH_TO_ARCH[t.goarch]}`;
const exeName = (t) => (t.goos === 'windows' ? 'gitglance.exe' : 'gitglance');

function run(cmd, args, opts = {}) {
  console.log(`$ ${cmd} ${args.join(' ')}`);
  const r = spawnSync(cmd, args, { stdio: 'inherit', ...opts });
  if (r.error) throw r.error;
  if (r.status !== 0) throw new Error(`${cmd} ${args.join(' ')} exited with ${r.status}`);
}

function findGo() {
  const candidates = [process.env.GO_BIN, 'go', 'D:/dev/go/bin/go.exe'].filter(Boolean);
  for (const c of candidates) {
    const r = spawnSync(c, ['version'], { stdio: 'ignore' });
    if (!r.error && r.status === 0) return c;
  }
  throw new Error('go not found: put it on PATH or set GO_BIN');
}

function version() {
  return JSON.parse(readFileSync(p('vscode', 'package.json'), 'utf8')).version;
}

function buildUi() {
  const vite = p('ui', 'node_modules', 'vite', 'bin', 'vite.js');
  if (!existsSync(vite)) throw new Error('vite is missing: run "npm ci" in ui first');
  run(process.execPath, [vite, 'build'], { cwd: p('ui') });
  const src = p('ui', 'dist');
  const web = p('core', 'internal', 'web', 'dist');
  mkdirSync(web, { recursive: true });
  for (const name of readdirSync(web)) {
    if (name !== '.keep') rmSync(join(web, name), { recursive: true, force: true });
  }
  cpSync(src, web, { recursive: true });
  const media = p('vscode', 'media', 'ui');
  rmSync(media, { recursive: true, force: true });
  cpSync(src, media, { recursive: true });
}

function buildCore(targets) {
  const go = findGo();
  const ver = version();
  for (const t of targets) {
    const out = p('dist', 'core', `${t.goos}-${t.goarch}`);
    rmSync(out, { recursive: true, force: true });
    mkdirSync(out, { recursive: true });
    const exe = join(out, exeName(t));
    run(go, ['build', '-trimpath', '-ldflags', `-s -w -X main.version=${ver}`, '-o', exe, './cmd/gitglance'], {
      cwd: p('core'),
      env: { ...process.env, CGO_ENABLED: '0', GOOS: t.goos, GOARCH: t.goarch },
    });
    const bin = p('vscode', 'bin', label(t));
    rmSync(bin, { recursive: true, force: true });
    mkdirSync(bin, { recursive: true });
    cpSync(exe, join(bin, exeName(t)));
  }
}

function buildVsix(targets) {
  const ver = version();
  run(process.execPath, ['esbuild.mjs'], { cwd: p('vscode') });
  const vsce = p('vscode', 'node_modules', '@vscode', 'vsce', 'vsce');
  if (!existsSync(vsce)) throw new Error('vsce is missing: run "npm ci" in vscode first');
  mkdirSync(p('dist'), { recursive: true });
  for (const t of targets) {
    const name = label(t);
    const bin = p('vscode', 'bin', name, exeName(t));
    if (!existsSync(bin)) throw new Error(`missing ${bin}: run the core step first`);
    if (!existsSync(p('vscode', 'media', 'ui'))) throw new Error('missing vscode/media/ui: run the ui step first');
    const pkg = p('dist', 'pkg', name);
    rmSync(pkg, { recursive: true, force: true });
    mkdirSync(join(pkg, 'dist'), { recursive: true });
    mkdirSync(join(pkg, 'bin', name), { recursive: true });
    mkdirSync(join(pkg, 'media'), { recursive: true });
    for (const f of ['package.json', 'package.nls.json', 'package.nls.zh-cn.json', 'README.md']) {
      cpSync(p('vscode', f), join(pkg, f));
    }
    cpSync(p('LICENSE'), join(pkg, 'LICENSE'));
    cpSync(p('vscode', 'l10n'), join(pkg, 'l10n'), { recursive: true });
    cpSync(p('vscode', 'media', 'icon.svg'), join(pkg, 'media', 'icon.svg'));
    cpSync(p('vscode', 'media', 'ui'), join(pkg, 'media', 'ui'), { recursive: true });
    cpSync(p('vscode', 'dist', 'extension.js'), join(pkg, 'dist', 'extension.js'));
    cpSync(bin, join(pkg, 'bin', name, exeName(t)));
    writeFileSync(join(pkg, '.vscodeignore'), '**/*.map\n');
    const out = p('dist', `git-glance-${name}-${ver}.vsix`);
    rmSync(out, { force: true });
    run(process.execPath, [vsce, 'package', '--target', name, '--no-dependencies', '--out', out], { cwd: pkg });
    rmSync(pkg, { recursive: true, force: true });
  }
  rmSync(p('dist', 'pkg'), { recursive: true, force: true });
}

function main() {
  const args = process.argv.slice(2);
  const step = args[0];
  const ti = args.indexOf('--targets');
  const mode = ti >= 0 ? args[ti + 1] : 'host';
  if (!['ui', 'core', 'vsix', 'all'].includes(step) || !['host', 'all'].includes(mode)) {
    console.error('usage: node scripts/build.mjs <ui|core|vsix|all> [--targets host|all]');
    process.exit(2);
  }
  const targets = mode === 'all' ? ALL_TARGETS : [hostTarget()];
  if (step === 'all') {
    rmSync(p('dist'), { recursive: true, force: true });
  }
  if (step === 'ui' || step === 'all') buildUi();
  if (step === 'core' || step === 'all') buildCore(targets);
  if (step === 'vsix' || step === 'all') buildVsix(targets);
  if (existsSync(p('dist'))) {
    for (const f of readdirSync(p('dist'), { recursive: true })) {
      const full = p('dist', f);
      if (statSync(full).isFile() && (f.endsWith('.vsix') || /gitglance(\.exe)?$/.test(f))) {
        console.log(`${f}  ${statSync(full).size} bytes`);
      }
    }
  }
}

try {
  main();
} catch (e) {
  console.error(e.message);
  process.exit(1);
}
