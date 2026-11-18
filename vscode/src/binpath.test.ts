import { describe, expect, it } from 'vitest';
import { coreBinaryPath } from './binpath';

describe('coreBinaryPath', () => {
  it('adds .exe on windows', () => {
    expect(coreBinaryPath('/x', 'win32', 'x64')).toBe('/x/bin/win32-x64/gitglance.exe');
  });
  it('has no extension elsewhere', () => {
    expect(coreBinaryPath('/x', 'darwin', 'arm64')).toBe('/x/bin/darwin-arm64/gitglance');
  });
});
