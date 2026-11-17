package ops

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
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
	if !validName(ctx, r, repoRoot, branch) {
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
	if shadowsRemote(snap, branch) {
		return refused("bad-name", "", nil), nil
	}
	if branchTaken(snap, branch) {
		return refused("branch-exists", "", nil), nil
	}
	for _, w := range snap.Worktrees {
		if insidePath(dir, w.Path) {
			return refused("path-inside-folder", w.Path, nil), nil
		}
	}
	if _, err := os.Lstat(dir); err == nil {
		return refused("path-exists", dir, nil), nil
	} else if !os.IsNotExist(err) {
		return failed(), err
	}
	cmd := []string{"worktree", "add", "--no-track", "-b", branch, dir, trunkRef}
	if _, err := r.Run(ctx, repoRoot, cmd...); err != nil {
		return failed(), err
	}
	res := OpResult{OK: true, Files: []string{}, Path: dir}
	res.LogError = remember(ctx, r, st, repoRoot, trunkName, branch, dir, cmd, now)
	return res, nil
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

// remember stores the base of the new branch and logs the operation. It
// returns the text of the failures, or "".
func remember(ctx context.Context, r gitx.Runner, st settings.Store, repoRoot, trunkName, branch, dir string, cmd []string, now func() time.Time) string {
	var msgs []string
	rs, err := st.Repo(repoRoot)
	if err == nil {
		if rs.CreatedBase == nil {
			rs.CreatedBase = map[string]string{}
		}
		rs.CreatedBase[branch] = trunkName
		err = st.SaveRepo(repoRoot, rs)
	}
	if err != nil {
		msgs = append(msgs, err.Error())
	}
	after, _ := revParse(ctx, r, repoRoot, "refs/heads/"+branch)
	if err := st.AppendOp(settings.OpEntry{
		Time: now().Unix(), Repo: repoRoot, Op: "create", Branch: branch, Folder: dir,
		Command: append([]string{"git"}, cmd...), After: after, Result: "ok",
	}); err != nil {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}
