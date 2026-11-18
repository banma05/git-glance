import type { zh } from './zh-CN';

export type Key = keyof typeof zh;

export const en: Record<Key, string> = {
  'app.name': 'Git Glance',
  'app.loading': 'Loading…',
  'app.retry': 'Retry',
  'app.cancel': 'Cancel',
  'app.confirm': 'OK',
  'app.close': 'Close',

  'error.noToken': 'Open the link printed in the terminal',
  'error.generic': 'Something went wrong: {message}',

  'chip.unpushed': 'Never pushed',
  'chip.behind': 'Behind remote by {n}',
  'chip.ahead': 'Ahead of remote by {n}',
  'chip.synced': 'In sync with remote',
  'chip.dirty': '{n} changes',
  'chip.testAll': 'On test',
  'chip.testSome': 'Partly on test {m}/{n}',
  'chip.testNone': 'Not on test',
  'chip.releaseAll': 'Released',
  'chip.releaseSome': 'Partly released {m}/{n}',
  'chip.releaseNone': 'Not released',
  'chip.removable': 'Folder can be removed',
  'chip.folderMissing': 'Folder no longer exists',

  'block.notInFolder': 'This branch has no folder of its own',
  'block.mainFolder': 'This is the main folder and cannot be removed',
  'block.folderMissing': 'The folder no longer exists',
  'block.dirty': 'The folder has uncommitted changes',
  'block.unpushed': 'There are commits that were not pushed',
  'block.notMerged': 'Not merged into the trunk yet',
};
