# Git Glance

See every branch of a repository at a glance: which trunks each branch has been merged into, what is behind, and what is safe to align.

- Sidebar view and a wide tab with the branch graph, details, branch-name copy and comparison.
- Align local branches with their upstream by fast-forward only.
- Read-only on remotes: Git Glance only runs `fetch`. It never pushes, never deletes a branch and never changes remote settings.
- Folders with uncommitted, untracked or hidden changes are refused.

Open the Git Glance icon in the activity bar to start. The UI follows the editor language (English or Simplified Chinese).

Your repository's own hooks and fsmonitor run as they would with your own git.
