import * as path from 'node:path';

// coreBinaryPath is where the packaged core program for a platform lives.
export function coreBinaryPath(extRoot: string, platform: string = process.platform, arch: string = process.arch): string {
  const name = platform === 'win32' ? 'gitglance.exe' : 'gitglance';
  // posix join keeps the result stable no matter which system builds the string
  return path.posix.join(extRoot.split('\\').join('/'), 'bin', `${platform}-${arch}`, name);
}
