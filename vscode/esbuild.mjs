import { build } from 'esbuild';

const smoke = process.argv.includes('--smoke');

await build({
  entryPoints: ['src/extension.ts'],
  bundle: true,
  platform: 'node',
  format: 'cjs',
  target: 'node18',
  outfile: 'dist/extension.js',
  external: ['vscode'],
  sourcemap: true,
  logLevel: 'info',
});

if (smoke) {
  await build({
    entryPoints: { runTest: 'test/runTest.ts', 'suite/index': 'test/suite/index.ts', 'suite/smoke.test': 'test/suite/smoke.test.ts' },
    bundle: true,
    platform: 'node',
    format: 'cjs',
    target: 'node18',
    outdir: 'dist-test',
    external: ['vscode', 'mocha'],
    logLevel: 'info',
  });
}
