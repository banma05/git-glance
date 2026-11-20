// Mirrors the JSON the Go core sends. Field names are the json tags of the Go
// structs; slices are never null, so arrays here are plain arrays. Pointers in
// Go (marshalled as null when unset) are `| null`.

// core/internal/repo
export interface CommitInfo {
  hash: string;
  subject: string;
  time: number;
  author: string; // author name (%an)
}

export interface DirtyCounts {
  changed: number;
  untracked: number;
  files: string[]; // first 50 paths
}

// core/internal/model
export type ContainState = 'all' | 'some' | 'none';

export interface CommitMatch extends CommitInfo {
  by: string; // empty when no step matched
}

export interface Containment {
  state: ContainState;
  matched: number;
  total: number;
  per: CommitMatch[];
}

export interface ReleaseSummary {
  state: ContainState;
  in: string[];
  notIn: string[];
}

export type TrunkSource = string; // how the trunk was chosen; "unknown" when there is none

export type RemoveBlock =
  | ''
  | 'not-in-folder'
  | 'main-folder'
  | 'folder-missing'
  | 'locked-worktree'
  | 'dirty'
  | 'unpushed'
  | 'not-merged'
  | 'error';

export interface BranchView {
  name: string;
  short: string;
  topic: string;
  color: number;
  trunk: string; // "" when unknown
  trunkSource: TrunkSource;
  folder: string; // "" when the branch has no folder
  folderHead: string; // HEAD hash of the folder; "" when there is none or it is missing or broken
  mainFolder: boolean;
  folderMissing: boolean;
  folderBroken: boolean; // the folder exists but git cannot read it (folderMissing is true too)
  folderLocked: boolean; // `git worktree lock`
  dirty: DirtyCounts | null; // set only when the folder exists and has changes
  hasUpstream: boolean;
  upstreamGone: boolean;
  ahead: number;
  behind: number;
  unpushed: number;
  behindTrunk: number;
  own: CommitInfo[];
  forkHash: string;
  forkTime: number;
  merged: boolean;
  test: Containment | null;
  release: ReleaseSummary | null;
  canRemoveFolder: boolean;
  removeBlock: RemoveBlock | string;
  old: boolean;
  lastCommit: number;
  error: string; // "" normally; why the details of the branch could not be read
}

export interface TrunkView {
  name: string;
  ref: string; // the name when it is compared through the local branch, else "<remote>/<name>"
  hash: string;
  time: number;
  hasLocal: boolean;
  localAhead: number;
  localBehind: number;
  folder: string;
  test: string;
  releases: string[];
}

export interface Group {
  trunk: TrunkView;
  branches: string[];
}

// core/internal/graph
export type RowKind = 'head' | 'commit' | 'more-own' | 'fork' | 'merge' | 'more';

export interface Row {
  kind: RowKind;
  lane: number;
  branch: string;
  hash: string;
  time: number;
  count: number;
  ends: number[]; // fork rows: lanes that join the trunk here
}

export interface Section {
  trunk: string;
  rows: Row[];
  lanes: number;
}

// core/internal/settings
export interface TrunkConfig {
  name: string;
  test: string;
  releases: string[];
}

export interface RepoSettings {
  trunks: TrunkConfig[];
  hidePrefix: string;
  oldDays: number;
  manualTrunk: Record<string, string>;
  createdBase: Record<string, string>;
}

export type Theme = 'auto' | 'dark' | 'light';

export interface Global {
  repos: string[];
  editor: string; // code|cursor|windsurf|idea|pycharm|webstorm|goland
  theme: Theme;
}

export interface OpEntry {
  time: number;
  repo: string;
  op: string;
  branch: string;
  folder: string;
  command: string[];
  before: string;
  after: string;
  result: string;
  detail: string;
}

export interface View {
  repo: string;
  settings: RepoSettings;
  groups: Group[];
  unknown: string[];
  old: string[];
  branches: Record<string, BranchView>;
  sections: Section[]; // same order as groups
  fetchedAt: number;
  fetchError: string;
}

// core/internal/ops
export interface FetchResult {
  ok: boolean;
  attempts: number;
  error: string;
  at: number; // unix seconds
}

export interface AlignItem {
  branch: string;
  from: string;
  to: string;
  folder: string;
}

export interface AlignSkip {
  branch: string;
  reason: string;
  files: string[];
  detail: string;
}

export interface AlignReport {
  fetch: FetchResult;
  updated: AlignItem[];
  skipped: AlignSkip[];
  logError: string;
}

export interface OpResult {
  ok: boolean;
  reason: string;
  files: string[];
  path: string;
  detail: string;
  logError: string;
}

// Shelf is uncommitted work a switch shelved with git stash.
export interface Shelf {
  folder: string;
  branch: string; // the branch the work was on
  hash: string; // the stash commit (40 or 64 hex)
  base: string; // HEAD when it was shelved
  time: number;
  files: string[]; // its first 50 paths
  fileCount: number; // the number of all its paths, at least files.length
  // pending | kept | superseded | restored | restored-entry-kept | orphan;
  // only pending is ever put back automatically; orphan only in switchState (folder "")
  state: string;
  // "" | base-moved | not-on-branch | folder-dirty | path-exists | check-failed | entry-gone |
  // apply-failed | apply-partial | apply-mismatch | entry-ambiguous | drop-failed | drop-shifted |
  // superseded | no-record; a base-moved shelf is always kept
  whyCode: string;
  why: string; // the English detail; "" exactly when whyCode is ""
  taken: string[]; // with whyCode path-exists, every path it would write where something is on disk; else []
}

// SwitchResult is the answer of switch. Every key is always there. Reasons
// beyond OpResult's: name-clash, update-verify-failed, track-verify-failed
// (the folder is on the new branch; shelved stays pending for the way back),
// shelf-failed, restore-failed, and unsaved-editors (made by the VS Code shell).
export interface SwitchResult extends OpResult {
  shelved: Shelf | null; // work shelved on the way out
  restored: Shelf | null; // work put back: the arrival branch's, or the just-taken one after a failed move
  keptShelf: Shelf | null; // work that was not put back; why says why
  fastForwarded: boolean;
  noFastForward: string; // "" | ahead | no-upstream | shelf-pending
  aheadNote: string; // "" or, with noFastForward ahead, the count and the upstream
}

// SwitchState is what the core knows of a folder's switches: the branch it
// was last switched away from ("" when none or already there) and the
// repository's shelves still in git stash list (every folder, orphans
// included), in stash order, newest first.
export interface SwitchState {
  previous: string;
  shelves: Shelf[];
}

export interface RemoveFolderResult {
  ok: boolean;
  reason: string;
  files: string[];
  backupDir: string;
  backupBytes: number;
  deletedBuild: string[];
  skippedLinks: string[]; // links the backup did not follow
  removedLinks: string[]; // links removed (the link only) before the folder went, or before links-failed
  detail: string;
  logError: string;
}

// core/internal/rpc: result shapes of the methods
export interface VersionResult {
  core: string;
  git: string; // "" when git was not found
  gitOK: boolean;
}

export interface FileChange {
  path: string;
  status: string; // first letter of the git status, e.g. "M", "A", "D"
}

export interface CompareResult {
  onlyA: CommitInfo[]; // at most 200, newest first
  onlyB: CommitInfo[];
  files: FileChange[];
  filesTruncated: boolean;
}

export interface FileText {
  text: string;
  binary: boolean;
  truncated: boolean;
}

export interface FileDiffResult {
  diff: string;
  truncated: boolean;
}

// trunkNews: the trunk commits a branch lacks, and the files the trunk changed
// since the branch started.
export interface TrunkNews {
  commits: CommitInfo[]; // newest first, at most 200
  total: number; // all trunk commits the branch lacks
  base: string; // merge-base hash, "" when the histories share no commit
  tip: string; // trunk tip hash
  files: FileChange[]; // base..tip; [] when base is ""
  filesTruncated: boolean;
}

// commitFiles: the files of one commit against its first parent.
export interface CommitFiles {
  parent: string; // "" for a root commit (its files are all additions)
  files: FileChange[];
  truncated: boolean;
}

export interface BackupsInfo {
  dir: string;
  bytes: number;
}

export interface RpcError {
  // not-repo, bare-repo, unsafe-repo, bad-params, unknown-method, git-failed,
  // git-too-old, not-allowed, editor-not-found, internal
  code: string;
  message: string;
}

export interface RpcResponse<T = unknown> {
  id: number;
  result?: T;
  error?: RpcError;
}

// Result type of every rpc method, keyed by method name.
export interface Methods {
  version: VersionResult;
  discover: { repo: string };
  repos: string[];
  addRepo: string[];
  removeRepo: string[];
  view: View;
  fetch: FetchResult;
  align: AlignReport;
  switch: SwitchResult;
  switchState: SwitchState;
  createFolder: OpResult;
  removeFolder: RemoveFolderResult;
  compare: CompareResult;
  fileAt: FileText;
  fileDiff: FileDiffResult;
  setRole: RepoSettings;
  moveTrunk: RepoSettings;
  assign: RepoSettings;
  setRepoSettings: RepoSettings;
  getGlobal: Global;
  setGlobal: Global;
  ops: OpEntry[];
  defaultFolder: { path: string };
  remoteBranches: string[];
  trunkCommits: CommitInfo[];
  trunkNews: TrunkNews;
  recentCommits: CommitInfo[];
  commitFiles: CommitFiles;
  backups: BackupsInfo;
  clearBackups: Record<string, never>;
  openInEditor: Record<string, never>;
}
