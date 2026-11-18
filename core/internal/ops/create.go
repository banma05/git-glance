package ops

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/model"
	"github.com/banma05/git-glance/core/internal/repo"
	"github.com/banma05/git-glance/core/internal/settings"
)

var folderNameCleaner = strings.NewReplacer(
	":", "-", "*", "-", "?", "-", `"`, "-", "<", "-", ">", "-", "|", "-", `\`, "-",
)

func dashSpace(r rune) rune {
	if unicode.IsSpace(r) {
		return '-'
	}
	return r
}

// DefaultFolderPath is <parent of repoRoot>/<base of repoRoot>-wt-<topic of the
// branch, made safe for a folder name>, with forward slashes on every system.
func DefaultFolderPath(repoRoot, branch string) string {
	root := filepath.ToSlash(filepath.Clean(repoRoot))
	topic := strings.Map(dashSpace, folderNameCleaner.Replace(model.Topic(branch)))
	return path.Join(path.Dir(root), path.Base(root)+"-wt-"+topic)
}

// CreateFolder creates branch from trunkRef (a full ref name) in a new folder
// at dir, without upstream, and remembers trunkName as the branch's base.
func CreateFolder(ctx context.Context, r gitx.Runner, st settings.Store, repoRoot, trunkName, trunkRef, branch, dir string, now func() time.Time) (OpResult, error) {
	if !validName(ctx, r, repoRoot, branch) || ambiguousName(branch) {
		return refused("bad-name", "", nil), nil
	}
	if !strings.HasPrefix(trunkRef, "refs/remotes/") && !strings.HasPrefix(trunkRef, "refs/heads/") {
		return failed(), fmt.Errorf("trunk ref %q is not a full ref name", trunkRef)
	}
	if !filepath.IsAbs(dir) {
		return failed(), fmt.Errorf("folder path %q is not absolute", dir)
	}
	snap, err := repo.Read(ctx, r, repoRoot)
	if err != nil {
		return failed(), err
	}
	tag, err := shadowsTag(ctx, r, repoRoot, branch)
	if err != nil {
		return failed(), err
	}
	if tag || shadowsRemote(snap, branch) {
		return refused("bad-name", "", nil), nil
	}
	if branchTaken(snap, branch) {
		return refused("branch-exists", "", nil), nil
	}
	if res, done, err := checkNewFolder(snap, dir); done || err != nil {
		return res, err
	}
	cmd := []string{"worktree", "add", "--no-track", "-b", branch, dir, trunkRef}
	entry := createEntry(repoRoot, branch, dir, cmd, now)
	if _, err := r.Run(ctx, repoRoot, cmd...); err != nil {
		// git creates the branch before it checks out the folder, so a
		// failure can leave it behind. It holds nothing but its start
		// commit; it is kept, never deleted, and named with its hash.
		kept := ""
		if hash, ok := branchHash(ctx, r, repoRoot, branch); ok {
			entry.After = hash
			kept = "branch " + branch + " was created at " + hash + " and kept"
		}
		return createFailed(st, dir, entry, err, kept), nil
	}
	return finishCreate(ctx, r, st, repoRoot, trunkName, branch, dir, entry), nil
}

// checkNewFolder refuses a folder path inside an existing folder of the
// repository (never write into the user's working tree) or one that exists.
// done is true when res is a refusal.
func checkNewFolder(snap repo.Snapshot, dir string) (OpResult, bool, error) {
	for _, w := range snap.Worktrees {
		if insidePath(dir, w.Path) {
			return refused("path-inside-folder", w.Path, nil), true, nil
		}
	}
	if _, err := os.Lstat(dir); err == nil {
		return refused("path-exists", dir, nil), true, nil
	} else if !os.IsNotExist(err) {
		return failed(), true, err
	}
	return OpResult{}, false, nil
}

// CreateFolderForBranch checks an existing local branch out in a new folder
// at dir. The branch must exist and no folder may hold it, nor a rebase or
// bisect work on it; the path checks are those of CreateFolder. The branch is
// checked again right before git runs, as git would otherwise create one
// (from a remote branch of the same name).
func CreateFolderForBranch(ctx context.Context, r gitx.Runner, st settings.Store, repoRoot, branch, dir string, now func() time.Time) (OpResult, error) {
	if !validName(ctx, r, repoRoot, branch) {
		return refused("bad-name", "", nil), nil
	}
	if !filepath.IsAbs(dir) {
		return failed(), fmt.Errorf("folder path %q is not absolute", dir)
	}
	snap, err := repo.Read(ctx, r, repoRoot)
	if err != nil {
		return failed(), err
	}
	if res, done, err := checkExistingBranch(ctx, r, repoRoot, snap, branch); done || err != nil {
		return res, err
	}
	if res, done, err := checkNewFolder(snap, dir); done || err != nil {
		return res, err
	}
	before, ok := branchHash(ctx, r, repoRoot, branch)
	if !ok {
		return refused("no-branch", "", nil), nil
	}
	cmd := []string{"worktree", "add", dir, branch}
	entry := createEntry(repoRoot, branch, dir, cmd, now)
	entry.Before = before
	if _, err := r.Run(ctx, repoRoot, cmd...); err != nil {
		entry.After, _ = branchHash(ctx, r, repoRoot, branch)
		return createFailed(st, dir, entry, err, ""), nil
	}
	return finishCreate(ctx, r, st, repoRoot, "", branch, dir, entry), nil
}

// checkExistingBranch refuses a branch that is not a local branch
// (no-branch), that a folder holds, missing folders included
// (checked-out-elsewhere, Path is the folder), or that a rebase or bisect
// works on (in-progress). done is true when res is a refusal.
func checkExistingBranch(ctx context.Context, r gitx.Runner, repoRoot string, snap repo.Snapshot, branch string) (OpResult, bool, error) {
	if !slices.ContainsFunc(snap.Local, func(b repo.Ref) bool { return b.Name == branch }) {
		return refused("no-branch", "", nil), true, nil
	}
	for _, w := range snap.Worktrees {
		if w.Branch == branch && !w.Detached {
			return refused("checked-out-elsewhere", w.Path, nil), true, nil
		}
	}
	held, err := heldBranches(ctx, r, repoRoot)
	if err != nil {
		return failed(), true, err
	}
	if held[branch] {
		return refused("in-progress", "", nil), true, nil
	}
	return OpResult{}, false, nil
}

func createEntry(repoRoot, branch, dir string, cmd []string, now func() time.Time) settings.OpEntry {
	return settings.OpEntry{
		Time: now().Unix(), Repo: repoRoot, Op: "create", Branch: branch, Folder: dir,
		Command: append([]string{"git"}, cmd...),
	}
}

// createFailed reports and logs a worktree add that git ran and failed.
func createFailed(st settings.Store, dir string, entry settings.OpEntry, err error, note string) OpResult {
	res := OpResult{Reason: "git-failed", Files: []string{}, Path: dir, Detail: joinDetail(failDetail(err), note)}
	res.LogError = logRan(st, entry, gitFailed(err), note)
	return res
}

// finishCreate checks that the new folder is on branch, remembers trunkName
// (when set) as the branch's base and logs the operation.
func finishCreate(ctx context.Context, r gitx.Runner, st settings.Store, repoRoot, trunkName, branch, dir string, entry settings.OpEntry) OpResult {
	entry.After, _ = branchHash(ctx, r, repoRoot, branch)
	if _, why := checkOnBranch(ctx, r, dir, "refs/heads/"+branch); why != "" {
		res := OpResult{Reason: "verify-failed", Files: []string{}, Path: dir, Detail: why}
		res.LogError = logRan(st, entry, verifyFailed(why), "")
		return res
	}
	res := OpResult{OK: true, Files: []string{}, Path: dir}
	res.LogError = remember(st, repoRoot, trunkName, branch, entry)
	return res
}

// branchHash returns the commit of a local branch, if it exists.
func branchHash(ctx context.Context, r gitx.Runner, repoRoot, branch string) (string, bool) {
	out, err := r.Run(ctx, repoRoot, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	h := strings.TrimSpace(string(out))
	return h, err == nil && h != ""
}

// validName accepts only a plain branch name: git must approve it and print it
// back unchanged, so shortcuts such as "@{-1}" are refused.
func validName(ctx context.Context, r gitx.Runner, repoRoot, branch string) bool {
	if branch == "" || strings.HasPrefix(branch, "-") {
		return false
	}
	out, err := r.Run(ctx, repoRoot, "check-ref-format", "--branch", branch)
	return err == nil && strings.TrimSpace(string(out)) == branch
}

// ambiguousName is true for a name git would read as something else: one
// whose first segment is a ref namespace (refs, heads, tags, remotes), or a
// pseudo-ref (HEAD, or a name ending in _HEAD such as FETCH_HEAD). Case is
// ignored: on a case-insensitive file system "head" finds .git/HEAD.
func ambiguousName(branch string) bool {
	first, _, _ := strings.Cut(branch, "/")
	switch strings.ToLower(first) {
	case "refs", "heads", "tags", "remotes":
		return true
	}
	upper := strings.ToUpper(branch)
	return upper == "HEAD" || strings.HasSuffix(upper, "_HEAD")
}

// shadowsTag is true when a tag has the same name as branch, ignoring case:
// git would then read the short name as the tag.
func shadowsTag(ctx context.Context, r gitx.Runner, repoRoot, branch string) (bool, error) {
	out, err := r.Run(ctx, repoRoot, "for-each-ref", "--format=%(refname:lstrip=2)", "refs/tags")
	if err != nil {
		return false, err
	}
	for _, tag := range strings.Split(string(out), "\n") {
		if strings.EqualFold(strings.TrimSpace(tag), branch) {
			return true, nil
		}
	}
	return false, nil
}

// shadowsRemote is true when branch would collide with the namespace of a
// remote-tracking branch: it equals one (origin/main) or starts with the same
// first path segment (origin, origin/x). Such a branch makes names like
// origin/main ambiguous for git. Case is ignored.
func shadowsRemote(snap repo.Snapshot, branch string) bool {
	first, _, _ := strings.Cut(branch, "/")
	for _, b := range snap.Remote {
		remote, _, _ := strings.Cut(b.Name, "/")
		if strings.EqualFold(branch, b.Name) || strings.EqualFold(first, remote) {
			return true
		}
	}
	return false
}

// branchTaken is true when a local branch has that name, or when a remote
// branch has it: that one belongs to somebody else. Case is ignored, because
// on case-insensitive systems git can silently move a packed branch whose name
// differs only in case.
func branchTaken(snap repo.Snapshot, branch string) bool {
	for _, b := range snap.Local {
		if strings.EqualFold(b.Name, branch) {
			return true
		}
	}
	for _, b := range snap.Remote {
		if _, name, ok := strings.Cut(b.Name, "/"); ok && strings.EqualFold(name, branch) {
			return true
		}
	}
	return false
}

// remember stores trunkName (when set) as the base of the new branch and logs
// the operation. It returns the text of the failures, or "".
func remember(st settings.Store, repoRoot, trunkName, branch string, entry settings.OpEntry) string {
	saveErr := ""
	if trunkName != "" {
		rs, err := st.Repo(repoRoot)
		if err == nil {
			if rs.CreatedBase == nil {
				rs.CreatedBase = map[string]string{}
			}
			rs.CreatedBase[branch] = trunkName
			err = st.SaveRepo(repoRoot, rs)
		}
		if err != nil {
			saveErr = err.Error()
		}
	}
	return joinDetail(saveErr, logRan(st, entry, "ok", ""))
}
