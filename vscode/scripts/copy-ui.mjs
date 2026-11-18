// Copies the built UI (../ui/dist) into media/ui. The target is gitignored.
import { cpSync, existsSync, rmSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const from = fileURLToPath(new URL('../../ui/dist', import.meta.url));
const to = fileURLToPath(new URL('../media/ui', import.meta.url));
if (!existsSync(from)) {
  console.error('ui/dist is missing: run "npm run build" in ui first');
  process.exit(1);
}
rmSync(to, { recursive: true, force: true });
cpSync(from, to, { recursive: true });
console.log('copied ui/dist to media/ui');
