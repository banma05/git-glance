package ops

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/repo"
	"github.com/banma05/git-glance/core/internal/settings"
)

// AlignItem is one branch moved forward to its upstream.
type AlignItem struct {
	Branch string `json:"branch"`
	From   string `json:"from"`
	To     string `json:"to"`
	Folder string `json:"folder"`
}

// AlignSkip is a branch that was left alone. Reason is one of dirty, ahead,
// diverged, upstream-gone, hidden-changes (tracked files marked skip-worktree or
// assume-unchanged exist on disk), folder-missing (its folder was deleted from disk),
// checked-out-twice (several folders hold the branch), tracks-other-branch
// (its upstream is a branch of another name, such as a trunk; Detail names
// it), in-progress (a stopped rebase or a bisect works on it), git-failed
// (Detail is git's message) or verify-failed (git ran but the branch or its
// folder is not where it should be afterwards; Detail says what).
type AlignSkip struct {
	Branch string   `json:"branch"`
	Reason string   `json:"reason"`
	Files  []string `json:"files"`
	Detail string   `json:"detail"`
}

// AlignReport is the outcome of Align. LogError is empty unless writing an
// operation log entry failed (the branch updates themselves still happened).
type AlignReport struct {
	Fetch    FetchResult `json:"fetch"`
	Updated  []AlignItem `json:"updated"`
	Skipped  []AlignSkip `json:"skipped"`
	LogError string      `json:"logError"`
}

const maxSkipFiles = 50

// Align fetches, then fast-forwards every local branch that is only behind its
// remote-tracking upstream of the same name. Anything that is not a pure
// fast-forward of a clean branch is left untouched and reported. A failed
// fetch (one remote unreachable, or offline) does not stop it: the
// remote-tracking refs that are there are still safe targets, and the report
// carries the fetch error.
func Align(ctx context.Context, r gitx.Runner, st settings.Store, repoRoot string, now func() time.Time) (AlignReport, error) {
	fetch := Fetch(ctx, r, repoRoot, now)
	snap, err := repo.Read(ctx, r, repoRoot)
	if err != nil {
		return AlignReport{Fetch: fetch, Updated: []AlignItem{}, Skipped: []AlignSkip{}}, err
	}
	rep, err := alignSnapshot(ctx, r, st, repoRoot, snap, now)
	rep.Fetch = fetch
	return rep, err
}

// alignSnapshot aligns the branches of snap. Every update re-checks that the
// repository still matches the snapshot.
func alignSnapshot(ctx context.Context, r gitx.Runner, st settings.Store, repoRoot string, snap repo.Snapshot, now func() time.Time) (AlignReport, error) {
	rep := AlignReport{Updated: []AlignItem{}, Skipped: []AlignSkip{}}
	held, err := heldBranches(ctx, r, repoRoot)
	if err != nil {
		return rep, err
	}
	remotes := map[string]bool{}
	for _, rr := range snap.Remote {
		remotes["refs/remotes/"+rr.Name] = true
	}
	for _, b := range snap.Local {
		// Only upstreams that are remote-tracking refs present now; branches
		// tracking a local branch are not listed.
		if !strings.HasPrefix(b.Upstream, "refs/remotes/") || (!b.UpstreamGone && !remotes[b.Upstream]) {
			continue
		}
		if !b.UpstreamGone && b.Behind == 0 {
			continue
		}
		if skip, ok := checkBranch(b, held); ok {
			rep.Skipped = append(rep.Skipped, skip)
			continue
		}
		holders := holdersOf(snap, b.Name)
		if skip, ok := checkHolders(b.Name, holders); ok {
			rep.Skipped = append(rep.Skipped, skip)
			continue
		}
		var wt *repo.Worktree
		if len(holders) == 1 {
			wt = holders[0]
		}
		if wt != nil {
			hidden, err := hiddenChanges(ctx, r, wt.Path)
			if err != nil {
				rep.Skipped = append(rep.Skipped, AlignSkip{Branch: b.Name, Reason: "git-failed", Files: []string{}, Detail: failDetail(err)})
				continue
			}
			if len(hidden) > 0 {
				rep.Skipped = append(rep.Skipped, AlignSkip{Branch: b.Name, Reason: "hidden-changes", Files: capFiles(hidden)})
				continue
			}
		}
		item, skip, logErr, ok := update(ctx, r, st, repoRoot, b, wt, now)
		if rep.LogError == "" {
			rep.LogError = logErr
		}
		if ok {
			rep.Updated = append(rep.Updated, item)
		} else {
			rep.Skipped = append(rep.Skipped, skip)
		}
	}
	return rep, nil
}

// checkBranch skips a branch whose upstream is gone or is a branch of another
// name (following a trunk would carry it into this branch), a branch a
// rebase or bisect works on, and one with commits of its own.
func checkBranch(b repo.Ref, held map[string]bool) (AlignSkip, bool) {
	skip := func(reason, detail string) (AlignSkip, bool) {
		return AlignSkip{Branch: b.Name, Reason: reason, Files: []string{}, Detail: detail}, true
	}
	switch {
	case b.UpstreamGone:
		return skip("upstream-gone", "")
	case b.UpstreamRemoteRef != "refs/heads/"+b.Name:
		return skip("tracks-other-branch", strings.TrimPrefix(b.Upstream, "refs/remotes/"))
	case held[b.Name]:
		return skip("in-progress", "")
	case b.Ahead > 0 && b.Behind > 0:
		return skip("diverged", "")
	case b.Ahead > 0:
		return skip("ahead", "")
	}
	return AlignSkip{}, false
}

// heldBranches returns the branches that a stopped rebase or a bisect in any
// folder of the repository works on, from the state files git keeps in each
// folder's own git directory. Missing folders count too.
func heldBranches(ctx context.Context, r gitx.Runner, repoRoot string) (map[string]bool, error) {
	out, err := r.Run(ctx, repoRoot, "rev-parse", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	common := strings.TrimSpace(string(out))
	if !filepath.IsAbs(common) {
		common = filepath.Join(repoRoot, common)
	}
	gitDirs := []string{common}
	entries, err := os.ReadDir(filepath.Join(common, "worktrees"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			gitDirs = append(gitDirs, filepath.Join(common, "worktrees", e.Name()))
		}
	}
	held := map[string]bool{}
	for _, dir := range gitDirs {
		for _, name := range []string{"rebase-merge/head-name", "rebase-apply/head-name", "BISECT_START", "rebase-merge/update-refs"} {
			data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			for _, b := range heldNames(name, string(data)) {
				held[b] = true
			}
		}
	}
	return held, nil
}

// heldNames reads the branch names out of one state file. update-refs (from
// `rebase --update-refs`) lists every branch it will move, one record of ref,
// old and new hash per branch; the others name one branch.
func heldNames(file, data string) []string {
	if !strings.HasSuffix(file, "update-refs") {
		if b := strings.TrimPrefix(strings.TrimSpace(data), "refs/heads/"); b != "" {
			return []string{b}
		}
		return nil
	}
	var names []string
	for _, line := range strings.Split(data, "\n") {
		if b, ok := strings.CutPrefix(strings.TrimSpace(line), "refs/heads/"); ok && b != "" {
			names = append(names, b)
		}
	}
	return names
}

// holdersOf returns every non-detached worktree that has branch checked out.
func holdersOf(snap repo.Snapshot, branch string) []*repo.Worktree {
	var out []*repo.Worktree
	for i := range snap.Worktrees {
		if snap.Worktrees[i].Branch == branch && !snap.Worktrees[i].Detached {
			out = append(out, &snap.Worktrees[i])
		}
	}
	return out
}

// checkHolders skips a branch whose folders are missing, dirty or more than one.
func checkHolders(branch string, holders []*repo.Worktree) (AlignSkip, bool) {
	for _, w := range holders {
		if w.Missing {
			return AlignSkip{Branch: branch, Reason: "folder-missing", Files: []string{}}, true
		}
	}
	for _, w := range holders {
		if w.Dirty.Changed+w.Dirty.Untracked > 0 {
			files := append([]string{}, w.Dirty.Files...)
			if len(files) > maxSkipFiles {
				files = files[:maxSkipFiles]
			}
			return AlignSkip{Branch: branch, Reason: "dirty", Files: files}, true
		}
	}
	if len(holders) > 1 {
		return AlignSkip{Branch: branch, Reason: "checked-out-twice", Files: []string{}}, true
	}
	return AlignSkip{}, false
}

func revParse(ctx context.Context, r gitx.Runner, dir, ref string) (string, error) {
	out, err := r.Run(ctx, dir, "rev-parse", ref)
	return strings.TrimSpace(string(out)), err
}

// headRef returns the full ref name HEAD points at in dir ("HEAD" when the
// folder is detached). Short names are ambiguous when a tag shares the name.
func headRef(ctx context.Context, r gitx.Runner, dir string) (string, error) {
	out, err := r.Run(ctx, dir, "rev-parse", "--symbolic-full-name", "HEAD")
	return strings.TrimSpace(string(out)), err
}

// wrongHead explains why the folder is not on branchRef, or returns "".
func wrongHead(head, branchRef string) string {
	switch head {
	case branchRef:
		return ""
	case "HEAD":
		return "the folder is now detached, not on " + branchRef
	}
	return "HEAD of the folder is now " + head + ", not " + branchRef
}

// update moves one branch and verifies, before and after, that exactly the
// reported branch moved from the snapshot hash to the upstream hash. A branch
// in a folder is merged there; a branch without a folder is moved by a local
// fetch. Once git has run, the attempt is logged whatever its outcome.
func update(ctx context.Context, r gitx.Runner, st settings.Store, repoRoot string, b repo.Ref, wt *repo.Worktree, now func() time.Time) (AlignItem, AlignSkip, string, bool) {
	skip := func(reason, detail, logErr string) (AlignItem, AlignSkip, string, bool) {
		return AlignItem{}, AlignSkip{Branch: b.Name, Reason: reason, Files: []string{}, Detail: detail}, logErr, false
	}
	fail := func(detail string) (AlignItem, AlignSkip, string, bool) { return skip("git-failed", detail, "") }
	failErr := func(err error) (AlignItem, AlignSkip, string, bool) { return fail(failDetail(err)) }
	branchRef, upRef := "refs/heads/"+b.Name, b.Upstream
	dir, folder := repoRoot, ""
	cmd := []string{"fetch", ".", upRef + ":" + branchRef}
	if wt != nil {
		dir, folder = wt.Path, wt.Path
		cmd = []string{"merge", "--ff-only", "--no-overwrite-ignore", "--no-autostash", upRef}
		head, err := headRef(ctx, r, dir)
		if err != nil {
			return failErr(err)
		}
		if why := wrongHead(head, branchRef); why != "" {
			return fail(why)
		}
	}
	now0, err := revParse(ctx, r, repoRoot, branchRef)
	if err != nil {
		return failErr(err)
	}
	if now0 != b.Hash {
		return fail("branch changed since it was read: " + b.Hash + " -> " + now0)
	}
	entry := settings.OpEntry{
		Time: now().Unix(), Repo: repoRoot, Op: "align", Branch: b.Name, Folder: folder,
		Command: append([]string{"git"}, cmd...), Before: b.Hash,
	}
	if _, err := r.Run(ctx, dir, cmd...); err != nil {
		entry.After, _ = revParse(ctx, r, repoRoot, branchRef)
		return skip("git-failed", failDetail(err), logRan(st, entry, gitFailed(err), ""))
	}
	after, why := checkAligned(ctx, r, repoRoot, dir, branchRef, upRef, wt != nil)
	entry.After = after
	if why != "" {
		return skip("verify-failed", why, logRan(st, entry, verifyFailed(why), ""))
	}
	logErr := logRan(st, entry, "ok", "")
	return AlignItem{Branch: b.Name, From: b.Hash, To: after, Folder: folder}, AlignSkip{}, logErr, true
}

// checkAligned reads where the branch is after the update and explains why
// that is not its upstream, or not checked out in its folder any more.
func checkAligned(ctx context.Context, r gitx.Runner, repoRoot, dir, branchRef, upRef string, inFolder bool) (after, why string) {
	after, err := revParse(ctx, r, repoRoot, branchRef)
	if err != nil {
		return "", "could not read " + branchRef + ": " + failDetail(err)
	}
	want, err := revParse(ctx, r, repoRoot, upRef)
	if err != nil {
		return after, "could not read " + upRef + ": " + failDetail(err)
	}
	if after != want {
		return after, "branch is at " + after + " after the update, upstream is at " + want
	}
	if inFolder {
		head, err := headRef(ctx, r, dir)
		if err != nil {
			return after, "could not read HEAD of the folder: " + failDetail(err)
		}
		if why := wrongHead(head, branchRef); why != "" {
			return after, why + " after the update"
		}
	}
	return after, ""
}
