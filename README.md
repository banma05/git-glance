# Git Glance

Git Glance shows every branch of a git repository at a glance: which trunks a branch has been merged into, how far it is behind, and which branches are safe to align. It lists the trunk commits a branch does not have yet, and with one click switches the main folder to the latest version of a trunk or branch and back again, shelving uncommitted work on the way. It has a Go core, a web UI, a VS Code extension and a browser mode.

[简体中文](README.zh-CN.md)

## Install from a VSIX

Build or download the VSIX for your platform (`git-glance-<platform>-<arch>-<version>.vsix`), then:

```
code --install-extension git-glance-win32-x64-0.1.0.vsix
```

Open the Git Glance icon in the activity bar. To remove it, uninstall the extension from the Extensions view.

## Requirements

- Git 2.30 or newer is required. With an older git, or none, Git Glance changes nothing: it cannot add a repository, read the remote, align or touch folders, and it says so under the toolbar.
- A repository with a working folder. Bare repositories are not supported yet.

## Browser mode

```
gitglance serve
```

The core starts a local web server and prints a link such as `http://127.0.0.1:<port>/#token=...`. The token is made for this run of `gitglance serve` and lives in the `#` fragment, so it is never sent to the server in a request line; the page keeps it for the browser tab's session and sends it in a header. Open the link in your browser. Options: `--port N`, `--no-open`.

## Build from source

Requirements: Go (see `core/go.mod`), Node.js 22 or newer.

```
cd ui && npm ci
cd ../vscode && npm ci
cd ..
node scripts/build.mjs all --targets host
```

`scripts/build.mjs <ui|core|vsix|all> [--targets host|all]` writes the core binaries to `dist/core/` and the VSIX files to `dist/`. `--targets all` builds Windows, macOS and Linux for x64 and arm64.

## What Git Glance never does

- It never pushes, never deletes or changes remote branches, and touches remotes only to `fetch`. Opening a repository, Retry after a failed read, Align, and "Switch to … latest" (in the details of a trunk or of a branch) run `git fetch --all --prune --no-prune-tags`: it updates your remote-tracking branches (`refs/remotes/...`) and removes those whose branch was deleted on the remote; as with a plain `git fetch`, tags pointing into the fetched history may be added. Your own branches are not changed by it and no tag is removed.
- It only fast-forwards.
- It refuses folders with uncommitted, untracked or hidden (assume-unchanged / skip-worktree) changes, except that a switch of the main folder can shelve uncommitted work after you confirm the list of files (see below).
- It never deletes a branch.

## Safety notes

- **Delete folder** (removing a worktree folder) copies ignored files to a backup first, except build-output folders: `node_modules`, `target`, `build`, `dist`, `out`, `.gradle`, `__pycache__`, `.pytest_cache`, `.mypy_cache`, `.tox`, `.next`, `.nuxt`, `coverage`, `.venv` and `venv`. The folder is deleted only after the copy succeeded. Right before the removal the folder is checked again: the same branch and commit as the confirm dialog showed, no uncommitted changes, nothing in progress (rebase, merge, cherry-pick, revert, bisect), no build-output folder holding a git repository of its own, no commits that only this folder holds, the folder itself not a link. After the backup it checks again the list of ignored files and the size and time of every copied file, the branch and commit, operations in progress, hidden-change flags, commits only this folder holds, git repositories inside build output, and whether the folder became a link; if anything changed, nothing is deleted. Changes inside build-output folders are not checked (they are deleted anyway). A tracked or untracked file changed at that very moment makes git refuse the removal; links inside the folder may already have been removed by then (only the links, never what they point to). Backups live in `<Git Glance home>/backups/`, where the home is `$GITGLANCE_HOME` if set, otherwise `git-glance` inside your user config directory (for example `~/.config/git-glance` on Linux, `~/Library/Application Support/git-glance` on macOS, `%AppData%\git-glance` on Windows). To clear them, delete that `backups` folder.
- **Shelving.** When you switch the main folder while it has uncommitted changes, Git Glance first lists the files and, once you confirm, shelves them with `git stash push --include-untracked` (new untracked files included, ignored files left alone; the entry's message starts with `git-glance shelf`). When the folder is switched back to that branch, the work is put back with `git stash apply --index` only if the branch has not moved, the folder is clean and nothing, ignored files included, sits where the work would be written; only then is that one entry dropped, found by its hash. Otherwise the work stays in `git stash list`, and Git Glance tells you which entry it is and, when it will not come back on its own, gives the command to put it back by hand and the branch the folder must be on first. While shelved work waits to come back to a branch, Align and "Switch to … latest" do not move that branch. Like any stash entry, it stays until you drop it or git's housekeeping (`git gc`) expires it.
- **Unsaved editors.** In VS Code, a switch is refused while files in that folder have unsaved changes in the editor. Browser mode cannot see your editors, so save your files before you switch from there.
- **Branches from a remote.** "Switch to … latest" for a trunk or test branch that exists only on the remote creates a local branch that tracks it. It refuses a name that git could confuse with an existing local branch differing only in case, a tag, or a remote.
- Your repository's own hooks and fsmonitor run as they would with your own git.
- Git Glance keeps its own settings, operation log and backups in the home directory above, and never writes files of its own into your repositories. The only changes it makes to a repository are the git operations you click.

## License

MIT
