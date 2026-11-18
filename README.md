# Git Glance

Git Glance shows every branch of a git repository at a glance: which trunks a branch has been merged into, how far it is behind, and which branches are safe to align. It has a Go core, a web UI, a VS Code extension and a browser mode.

[简体中文](README.zh-CN.md)

## Install from a VSIX

Build or download the VSIX for your platform (`git-glance-<platform>-<arch>-<version>.vsix`), then:

```
code --install-extension git-glance-win32-x64-0.1.0.vsix
```

Open the Git Glance icon in the activity bar. To remove it, uninstall the extension from the Extensions view.

## Browser mode

```
gitglance serve
```

The core starts a local web server and prints a link such as `http://127.0.0.1:<port>/#token=...`. The one-time token lives in the `#` fragment, so it is never sent to the server in a request line. Open the link in your browser. Options: `--port N`, `--no-open`.

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

- It never pushes, never deletes or changes remote branches, and touches remotes only to `fetch`.
- It only fast-forwards.
- It refuses folders with uncommitted, untracked or hidden (assume-unchanged / skip-worktree) changes.
- It never deletes a branch.

## Safety notes

- **Delete folder** (removing a worktree folder) copies ignored files to a backup first, except build output such as `node_modules`, `dist` and `target`. The folder is deleted only after the copy succeeded. Backups live in `<Git Glance home>/backups/`, where the home is `$GITGLANCE_HOME` if set, otherwise `git-glance` inside your user config directory (for example `~/.config/git-glance` on Linux, `~/Library/Application Support/git-glance` on macOS, `%AppData%\git-glance` on Windows). To clear them, delete that `backups` folder.
- Your repository's own hooks and fsmonitor run as they would with your own git.
- Git Glance keeps its own settings, operation log and backups in the home directory above, and never writes files of its own into your repositories. The only changes it makes to a repository are the git operations you click.

## License

MIT
