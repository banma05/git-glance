import * as path from 'node:path';
import Mocha from 'mocha';

export function run(): Promise<void> {
  const mocha = new Mocha({ ui: 'tdd', color: false, timeout: 60_000 });
  mocha.addFile(path.resolve(__dirname, 'smoke.test.js'));
  return new Promise((resolve, reject) => {
    mocha.run((failures) => (failures ? reject(new Error(`${failures} tests failed`)) : resolve()));
  });
}
