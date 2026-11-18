import { defineConfig } from 'vitest/config';
import { fileURLToPath } from 'node:url';

export default defineConfig({
  resolve: {
    alias: { vscode: fileURLToPath(new URL('./src/vscode-stub.ts', import.meta.url)) },
  },
  test: { environment: 'node', include: ['src/**/*.test.ts'] },
});
