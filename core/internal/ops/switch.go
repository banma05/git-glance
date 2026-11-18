package ops

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/repo"
	"github.com/banma05/git-glance/core/internal/settings"
)

// OpResult is the outcome of Switch and CreateFolder. When OK is false, Reason
// says why. Switch reasons: dirty (Files lists the changes),
// folder-missing, hidden-changes (Files lists tracked files marked skip-worktree or
// assume-unchanged that exist on disk, git status cannot show their edits),
// detached (the folder is on a detached HEAD, leaving it could
// orphan commits), checked-out-elsewhere (Path is that folder) and no-branch
// (no local branch of that name). CreateFolder reasons: bad-name, branch-exists,
// path-inside-folder (Path is the folder) and path-exists; CreateFolderForBranch
// reasons: bad-name, no-branch, checked-out-elsewhere (Path is that folder),
// in-progress (a rebase or bisect works on the branch), path-inside-folder and
// path-exists. Up to here nothing was done. All may also return git-failed
// (git ran and failed; Detail is its message and, for CreateFolder, names a
// branch git created and left behind) and verify-failed (git ran but the
// folder is not on the branch afterwards; Detail says what it is on). LogError
// is empty unless saving settings or the operation log failed.
type OpResult struct {
	OK       bool     `json:"ok"`
	Reason   string   `json:"reason"`
	Files    []string `json:"files"`
	Path     string   `json:"path"`
	Detail   string   `json:"detail"`
	LogError string   `json:"logError"`
}

func refused(reason, path string, files []string) OpResult {
	if files == nil {
		files = []string{}
	}
	return OpResult{Reason: reason, Files: files, Path: path}
}

func failed() OpResult { return OpResult{Files: []string{}} }

// normPath makes paths comparable: cleaned, forward slashes, and lower case on
// Windows where folder names ignore case.
func normPath(p string) string {
	p = filepath.ToSlash(filepath.Clean(p))
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

func samePath(a, b string) bool { return normPath(a) == normPath(b) }

// insidePath reports whether p is dir itself or lies below it.
func insidePath(p, dir string) bool {
	p, dir = normPath(p), normPath(dir)
	return p == dir || strings.HasPrefix(p, strings.TrimSuffix(dir, "/")+"/")
}

// Switch checks branch out in folder. It refuses when the folder is missing or
// has any change or new file, when another folder holds the branch, or when
// there is no local branch of that name (a remote-only branch is never created
// here). The decision is made on a snapshot and repeated right before git runs.
func Switch(ctx context.Context, r gitx.Runner, st settings.Store, repoRoot, folder, branch string, now func() time.Time) (OpResult, error) {
	branchRef := "refs/heads/" + branch
	var wt repo.Worktree
	for pass := 0; pass < 2; pass++ {
		snap, err := repo.Read(ctx, r, repoRoot)
		if err != nil {
			return failed(), err
		}
		var res OpResult
		var done bool
		wt, res, done, err = decideSwitch(snap, folder, branch)
		if err != nil || done {
			return res, err
		}
	}
	hidden, err := hiddenChanges(ctx, r, wt.Path)
	if err != nil {
		return failed(), err
	}
	if len(hidden) > 0 {
		return refused("hidden-changes", "", capFiles(hidden)), nil
	}
	before, err := headRef(ctx, r, wt.Path)
	if err != nil {
		return failed(), err
	}
	cmd := []string{"switch", "--no-guess", "--no-overwrite-ignore", branch}
	entry := settings.OpEntry{
		Time: now().Unix(), Repo: repoRoot, Op: "switch", Branch: branch, Folder: wt.Path,
		Command: append([]string{"git"}, cmd...), Before: before,
	}
	if _, err := r.Run(ctx, wt.Path, cmd...); err != nil {
		entry.After, _ = headRef(ctx, r, wt.Path)
		res := OpResult{Reason: "git-failed", Files: []string{}, Path: wt.Path, Detail: failDetail(err)}
		res.LogError = logRan(st, entry, gitFailed(err), "")
		return res, nil
	}
	head, why := checkOnBranch(ctx, r, wt.Path, branchRef)
	entry.After = head
	if why != "" {
		res := OpResult{Reason: "verify-failed", Files: []string{}, Path: wt.Path, Detail: why}
		res.LogError = logRan(st, entry, verifyFailed(why), "")
		return res, nil
	}
	res := OpResult{OK: true, Files: []string{}, Path: wt.Path}
	res.LogError = logRan(st, entry, "ok", "")
	return res, nil
}

// checkOnBranch reads the full ref the folder's HEAD is on and explains why it
// is not branchRef, or returns "".
func checkOnBranch(ctx context.Context, r gitx.Runner, folder, branchRef string) (head, why string) {
	head, err := headRef(ctx, r, folder)
	if err != nil {
		return "", "could not read HEAD of the folder: " + failDetail(err)
	}
	if head != branchRef {
		return head, fmt.Sprintf("the folder is on %s, not %s", head, branchRef)
	}
	return head, ""
}

// decideSwitch returns the target worktree. done is true when the answer is
// already final: a refusal, an error, or the folder is on the branch already.
func decideSwitch(snap repo.Snapshot, folder, branch string) (repo.Worktree, OpResult, bool, error) {
	var wt *repo.Worktree
	for i := range snap.Worktrees {
		if samePath(snap.Worktrees[i].Path, folder) {
			wt = &snap.Worktrees[i]
			break
		}
	}
	if wt == nil {
		return repo.Worktree{}, failed(), true, fmt.Errorf("%s is not a folder of this repository", folder)
	}
	if wt.Missing {
		return *wt, refused("folder-missing", wt.Path, nil), true, nil
	}
	known := false
	for _, b := range snap.Local {
		known = known || b.Name == branch
	}
	if !known {
		return *wt, refused("no-branch", "", nil), true, nil
	}
	for _, o := range snap.Worktrees {
		if o.Branch == branch && !o.Detached && !samePath(o.Path, wt.Path) {
			return *wt, refused("checked-out-elsewhere", o.Path, nil), true, nil
		}
	}
	if wt.Dirty.Changed+wt.Dirty.Untracked > 0 {
		files := append([]string{}, wt.Dirty.Files...)
		if len(files) > maxSkipFiles {
			files = files[:maxSkipFiles]
		}
		return *wt, refused("dirty", "", files), true, nil
	}
	if wt.Detached {
		return *wt, refused("detached", wt.Path, nil), true, nil
	}
	if wt.Branch == branch {
		return *wt, OpResult{OK: true, Files: []string{}, Path: wt.Path}, true, nil
	}
	return *wt, OpResult{}, false, nil
}
