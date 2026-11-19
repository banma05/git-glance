package ops

import (
	"context"
	"fmt"
	"strings"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/repo"
	"github.com/banma05/git-glance/core/internal/settings"
)

// maxOrphanLookups caps how many unrecorded stash entries are read to find
// orphans.
const maxOrphanLookups = 100

// SwitchState is what a folder can switch back to. Previous is the branch the
// tool last switched the folder away from: "" when there is none, when that
// branch is gone, or when the folder is on it. Shelves are the repository's
// shelves (of any folder) still in git's stash, in stash order (newest
// first), with their state; Git Glance entries without a record are listed
// as orphans.
type SwitchState struct {
	Previous string           `json:"previous"`
	Shelves  []settings.Shelf `json:"shelves"`
}

// ReadSwitchState reads the state for folder, which must be a folder of the
// repository. It changes nothing.
func ReadSwitchState(ctx context.Context, r gitx.Runner, st settings.Store, repoRoot, folder string) (SwitchState, error) {
	state := SwitchState{Shelves: []settings.Shelf{}}
	folders, err := repo.ListFolders(ctx, r, repoRoot)
	if err != nil {
		return state, err
	}
	var wt *repo.Worktree
	for i := range folders {
		if samePath(folders[i].Path, folder) {
			wt = &folders[i]
		}
	}
	if wt == nil {
		return state, fmt.Errorf("%s is not a folder of this repository", folder)
	}
	rec, err := st.SwitchRecords(repoRoot)
	if err != nil {
		return state, err
	}
	list, err := stashList(ctx, r, repoRoot)
	if err != nil {
		return state, err
	}
	if state.Shelves, err = listShelves(ctx, r, repoRoot, rec, list); err != nil {
		return state, err
	}
	state.Previous = previousOf(ctx, r, repoRoot, rec, wt.Path)
	return state, nil
}

// listShelves returns the shelves still in the stash, in stash order: the
// recorded ones, and Git Glance entries without a record as orphans.
func listShelves(ctx context.Context, r gitx.Runner, repoRoot string, rec settings.SwitchRecords, list []string) ([]settings.Shelf, error) {
	recorded := map[string]settings.Shelf{}
	for _, sh := range rec.Shelves {
		recorded[sh.Hash] = sh
	}
	var unrecorded []string
	for _, h := range list {
		if _, ok := recorded[h]; !ok && len(unrecorded) < maxOrphanLookups {
			unrecorded = append(unrecorded, h)
		}
	}
	orphans, err := readOrphans(ctx, r, repoRoot, unrecorded)
	if err != nil {
		return nil, err
	}
	out, seen := []settings.Shelf{}, map[string]bool{}
	for _, h := range list {
		if seen[h] {
			continue
		}
		seen[h] = true
		if sh, ok := recorded[h]; ok {
			out = append(out, sh)
		} else if o, ok := orphans[h]; ok {
			out = append(out, o)
		}
	}
	return out, nil
}

// readOrphans returns, of the given stash entries, those whose message is a
// Git Glance shelf message ("On <branch>: git-glance shelf <branch> <time>").
func readOrphans(ctx context.Context, r gitx.Runner, repoRoot string, hashes []string) (map[string]settings.Shelf, error) {
	orphans := map[string]settings.Shelf{}
	if len(hashes) == 0 {
		return orphans, nil
	}
	args := append([]string{"log", "--no-show-signature", "--no-walk=unsorted", "--format=%H%x00%P%x00%s"}, hashes...)
	out, err := r.Run(ctx, repoRoot, append(args, "--")...)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.SplitN(strings.TrimRight(line, "\r"), "\x00", 3)
		if len(f) != 3 {
			continue
		}
		_, msg, _ := strings.Cut(f[2], ": ")
		branch, at, ok := gitx.ParseShelfMessage(msg)
		parents := strings.Fields(f[1])
		if !ok || len(parents) == 0 {
			continue
		}
		orphans[f[0]] = settings.Shelf{
			Branch: branch, Hash: f[0], Base: parents[0], Time: at, Files: []string{},
			State: settings.ShelfOrphan, WhyCode: whyNoRecord,
			Why: "a Git Glance shelf without a record (the tool stopped before recording it); it is in git stash list",
		}
	}
	return orphans, nil
}

// previousOf returns the branch the tool last switched folder away from,
// unless that branch is gone or the folder is on it.
func previousOf(ctx context.Context, r gitx.Runner, repoRoot string, rec settings.SwitchRecords, folder string) string {
	prev := ""
	for _, p := range rec.Previous {
		if samePath(p.Folder, folder) {
			prev = p.Branch
		}
	}
	if prev == "" {
		return ""
	}
	if _, ok := branchHash(ctx, r, repoRoot, prev); !ok {
		return ""
	}
	if head, err := headRef(ctx, r, folder); err == nil && head == "refs/heads/"+prev {
		return ""
	}
	return prev
}
