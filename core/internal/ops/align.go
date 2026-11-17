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
// diverged, upstream-gone, folder-missing (its folder was deleted from disk)
// or git-failed.
type AlignSkip struct {
	Branch string   `json:"branch"`
	Reason string   `json:"reason"`
	Files  []string `json:"files"`
	Detail string   `json:"detail"`
}

// AlignReport is the outcome of Align.
type AlignReport struct {
	Fetch   FetchResult `json:"fetch"`
	Updated []AlignItem `json:"updated"`
	Skipped []AlignSkip `json:"skipped"`
}

const maxSkipFiles = 50

// Align fetches, then fast-forwards every local branch that is only behind its
// remote-tracking upstream. Anything that is not a pure fast-forward of a clean
// branch is left untouched and reported.
func Align(ctx context.Context, r gitx.Runner, st settings.Store, repoRoot string, now func() time.Time) (AlignReport, error) {
	rep := AlignReport{Updated: []AlignItem{}, Skipped: []AlignSkip{}}
	rep.Fetch = Fetch(ctx, r, repoRoot, now)
	if !rep.Fetch.OK {
		return rep, nil
	}
	snap, err := repo.Read(ctx, r, repoRoot)
	if err != nil {
		return rep, err
	}
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
		wt := checkedOutIn(snap, b.Name)
		if wt != nil {
			if wt.Missing {
				rep.Skipped = append(rep.Skipped, AlignSkip{Branch: b.Name, Reason: "folder-missing", Files: []string{}})
				continue
			}
			if wt.Dirty.Changed+wt.Dirty.Untracked > 0 {
				files := append([]string{}, wt.Dirty.Files...)
				if len(files) > maxSkipFiles {
					files = files[:maxSkipFiles]
				}
				rep.Skipped = append(rep.Skipped, AlignSkip{Branch: b.Name, Reason: "dirty", Files: files})
				continue
			}
		}
		item, skip, ok := update(ctx, r, st, repoRoot, b, wt, now)
		if ok {
			rep.Updated = append(rep.Updated, item)
		} else {
			rep.Skipped = append(rep.Skipped, skip)
		}
	}
	return rep, nil
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

func checkedOutIn(snap repo.Snapshot, branch string) *repo.Worktree {
	for i := range snap.Worktrees {
		if snap.Worktrees[i].Branch == branch && !snap.Worktrees[i].Detached {
			return &snap.Worktrees[i]
		}
	}
	return nil
}

// update moves one branch and verifies the result. A branch in a folder is
// merged there; a branch without a folder is moved by a local fetch.
func update(ctx context.Context, r gitx.Runner, st settings.Store, repoRoot string, b repo.Ref, wt *repo.Worktree, now func() time.Time) (AlignItem, AlignSkip, bool) {
	fail := func(err error) (AlignItem, AlignSkip, bool) {
		return AlignItem{}, AlignSkip{Branch: b.Name, Reason: "git-failed", Files: []string{}, Detail: lastLine(err)}, false
	}
	upRef := "refs/remotes/" + b.Upstream
	dir, folder := repoRoot, ""
	cmd := []string{"fetch", ".", upRef + ":refs/heads/" + b.Name}
	if wt != nil {
		dir, folder = wt.Path, wt.Path
		cmd = []string{"merge", "--ff-only", upRef}
	}
	if _, err := r.Run(ctx, dir, cmd...); err != nil {
		return fail(err)
	}
	out, err := r.Run(ctx, repoRoot, "rev-parse", "refs/heads/"+b.Name)
	if err != nil {
		return fail(err)
	}
	after := strings.TrimSpace(string(out))
	_ = st.AppendOp(settings.OpEntry{
		Time: now().Unix(), Repo: repoRoot, Op: "align", Branch: b.Name, Folder: folder,
		Command: append([]string{"git"}, cmd...), Before: b.Hash, After: after, Result: "ok",
	})
	return AlignItem{Branch: b.Name, From: b.Hash, To: after, Folder: folder}, AlignSkip{}, true
}
