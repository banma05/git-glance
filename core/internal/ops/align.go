package ops

import (
	"context"
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
// diverged, upstream-gone, folder-missing (its folder was deleted from disk),
// checked-out-twice (several folders hold the branch) or git-failed.
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
// remote-tracking upstream. Anything that is not a pure fast-forward of a clean
// branch is left untouched and reported.
func Align(ctx context.Context, r gitx.Runner, st settings.Store, repoRoot string, now func() time.Time) (AlignReport, error) {
	fetch := Fetch(ctx, r, repoRoot, now)
	if !fetch.OK {
		return AlignReport{Fetch: fetch, Updated: []AlignItem{}, Skipped: []AlignSkip{}}, nil
	}
	snap, err := repo.Read(ctx, r, repoRoot)
	if err != nil {
		return AlignReport{Fetch: fetch, Updated: []AlignItem{}, Skipped: []AlignSkip{}}, err
	}
	rep := alignSnapshot(ctx, r, st, repoRoot, snap, now)
	rep.Fetch = fetch
	return rep, nil
}

// alignSnapshot aligns the branches of snap. Every update re-checks that the
// repository still matches the snapshot.
func alignSnapshot(ctx context.Context, r gitx.Runner, st settings.Store, repoRoot string, snap repo.Snapshot, now func() time.Time) AlignReport {
	rep := AlignReport{Updated: []AlignItem{}, Skipped: []AlignSkip{}}
	remotes := map[string]bool{}
	for _, rr := range snap.Remote {
		remotes[rr.Name] = true
	}
	for _, b := range snap.Local {
		if b.Upstream == "" || (!b.UpstreamGone && !remotes[b.Upstream]) {
			continue
		}
		if b.UpstreamGone {
			rep.Skipped = append(rep.Skipped, AlignSkip{Branch: b.Name, Reason: "upstream-gone", Files: []string{}})
			continue
		}
		if b.Behind == 0 {
			continue
		}
		if skip, ok := checkAhead(b); ok {
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
	return rep
}

func checkAhead(b repo.Ref) (AlignSkip, bool) {
	switch {
	case b.Ahead > 0 && b.Behind > 0:
		return AlignSkip{Branch: b.Name, Reason: "diverged", Files: []string{}}, true
	case b.Ahead > 0:
		return AlignSkip{Branch: b.Name, Reason: "ahead", Files: []string{}}, true
	}
	return AlignSkip{}, false
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

func headBranch(ctx context.Context, r gitx.Runner, dir string) (string, error) {
	out, err := r.Run(ctx, dir, "symbolic-ref", "-q", "--short", "HEAD")
	return strings.TrimSpace(string(out)), err
}

// update moves one branch and verifies, before and after, that exactly the
// reported branch moved from the snapshot hash to the upstream hash. A branch
// in a folder is merged there; a branch without a folder is moved by a local
// fetch.
func update(ctx context.Context, r gitx.Runner, st settings.Store, repoRoot string, b repo.Ref, wt *repo.Worktree, now func() time.Time) (AlignItem, AlignSkip, string, bool) {
	fail := func(detail string) (AlignItem, AlignSkip, string, bool) {
		return AlignItem{}, AlignSkip{Branch: b.Name, Reason: "git-failed", Files: []string{}, Detail: detail}, "", false
	}
	failErr := func(err error) (AlignItem, AlignSkip, string, bool) { return fail(lastLine(err)) }
	branchRef, upRef := "refs/heads/"+b.Name, "refs/remotes/"+b.Upstream
	dir, folder := repoRoot, ""
	cmd := []string{"fetch", ".", upRef + ":" + branchRef}
	if wt != nil {
		dir, folder = wt.Path, wt.Path
		cmd = []string{"merge", "--ff-only", "--no-overwrite-ignore", upRef}
		head, err := headBranch(ctx, r, dir)
		if err != nil {
			return failErr(err)
		}
		if head != b.Name {
			return fail("HEAD of the folder is now " + head + ", not " + b.Name)
		}
	}
	now0, err := revParse(ctx, r, repoRoot, branchRef)
	if err != nil {
		return failErr(err)
	}
	if now0 != b.Hash {
		return fail("branch changed since it was read: " + b.Hash + " -> " + now0)
	}
	if _, err := r.Run(ctx, dir, cmd...); err != nil {
		return failErr(err)
	}
	after, err := revParse(ctx, r, repoRoot, branchRef)
	if err != nil {
		return failErr(err)
	}
	want, err := revParse(ctx, r, repoRoot, upRef)
	if err != nil {
		return failErr(err)
	}
	if after != want {
		return fail("branch is at " + after + " after the update, upstream is at " + want)
	}
	if wt != nil {
		if head, err := headBranch(ctx, r, dir); err != nil || head != b.Name {
			return fail("HEAD of the folder is not " + b.Name + " after the update")
		}
	}
	logErr := ""
	if err := st.AppendOp(settings.OpEntry{
		Time: now().Unix(), Repo: repoRoot, Op: "align", Branch: b.Name, Folder: folder,
		Command: append([]string{"git"}, cmd...), Before: b.Hash, After: after, Result: "ok",
	}); err != nil {
		logErr = err.Error()
	}
	return AlignItem{Branch: b.Name, From: b.Hash, To: after, Folder: folder}, AlignSkip{}, logErr, true
}
