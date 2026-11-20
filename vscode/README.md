# Git Glance

See every branch of a repository at a glance: which trunks each branch has been merged into, what is behind, and what is safe to align.

- Sidebar view and a wide tab with the branch graph, details, branch-name copy and comparison.
- Align local branches with their upstream by fast-forward only.
- Switch the main folder to the latest version of a trunk or branch with one click, and back again. Uncommitted work is shelved with `git stash` after you confirm the list of files, and put back on the way back when that cannot conflict. A switch is refused while files in that folder have unsaved changes in the editor.
- Read-only on remotes: Git Glance only runs `fetch`. It never pushes, never deletes a branch and never changes remote settings.
- Folders with uncommitted, untracked or hidden changes are refused, except that a switch of the main folder can shelve uncommitted work after you confirm.

Open the Git Glance icon in the activity bar to start. The UI follows the editor language (English or Simplified Chinese). Git 2.30 or newer is needed for any change; bare repositories are not supported yet.

Your repository's own hooks and fsmonitor run as they would with your own git.
