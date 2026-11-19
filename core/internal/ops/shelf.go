package ops

import (
	"context"
	"errors"
	"strings"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/settings"
)

// Shelf.WhyCode values: why a shelf was not put back, or why its stash entry
// was kept after it was put back. Shelf.Why says the same in English.
const (
	whyBaseMoved      = "base-moved"      // the branch moved since the work was shelved
	whyNotOnBranch    = "not-on-branch"   // the folder is not on the shelf's branch
	whyFolderDirty    = "folder-dirty"    // the folder has changes
	whyPathExists     = "path-exists"     // a path the shelf would create is on disk
	whyCheckFailed    = "check-failed"    // the folder or the stash could not be read
	whyEntryGone      = "entry-gone"      // the entry is no longer in git stash list
	whyApplyFailed    = "apply-failed"    // git stash apply failed and changed nothing
	whyApplyPartial   = "apply-partial"   // git stash apply failed half way
	whyApplyMismatch  = "apply-mismatch"  // git stash apply ran, but the folder is not the shelf
	whyEntryAmbiguous = "entry-ambiguous" // the entry is listed more than once
	whyDropFailed     = "drop-failed"     // git stash drop failed
	whyDropShifted    = "drop-shifted"    // git dropped another entry, which was stored again
	whySuperseded     = "superseded"      // a newer shelf of the same folder and branch was taken
	whyNoRecord       = "no-record"       // a Git Glance entry without a record (orphan)
)

// folderStatus is what git status says about a folder: every changed, staged
// or untracked path, the submodules with changes of their own and the
// unmerged paths. An untracked folder holding a repository of its own is
// left out: git stash leaves it in place, like an ignored file.
type folderStatus struct {
	files, submodules, unmerged []string
}

func (f folderStatus) clean() bool { return len(f.files) == 0 }

func readFolderStatus(ctx context.Context, r gitx.Runner, dir string) (folderStatus, error) {
	out, err := r.Run(ctx, dir, "status", "--porcelain=v2", "-z", "--untracked-files=all")
	if err != nil {
		return folderStatus{}, err
	}
	return parseFolderStatus(string(out)), nil
}

// parseFolderStatus reads NUL-separated porcelain v2 entries. A renamed or
// copied entry is followed by one token holding the original path. An entry
// it does not understand counts as a change.
func parseFolderStatus(out string) folderStatus {
	var st folderStatus
	add := func(path, sub string, unmerged bool) {
		st.files = append(st.files, path)
		if strings.HasPrefix(sub, "S") {
			st.submodules = append(st.submodules, path)
		}
		if unmerged {
			st.unmerged = append(st.unmerged, path)
		}
	}
	toks := strings.Split(out, "\x00")
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		f := strings.Split(t, " ")
		switch {
		case t == "":
		case strings.HasPrefix(t, "? "):
			if !strings.HasSuffix(t, "/") {
				st.files = append(st.files, t[2:])
			}
		case t[0] == '1' && len(f) >= 9:
			add(strings.SplitN(t, " ", 9)[8], f[2], false)
		case t[0] == '2' && len(f) >= 10:
			add(strings.SplitN(t, " ", 10)[9], f[2], false)
			i++
		case t[0] == 'u' && len(f) >= 11:
			add(strings.SplitN(t, " ", 11)[10], f[2], true)
		default:
			st.files = append(st.files, t)
		}
	}
	return st
}

// hasRev reports whether rev names a commit; a failure other than "no such
// commit" is an error.
func hasRev(ctx context.Context, r gitx.Runner, dir, rev string) (string, bool, error) {
	out, err := r.Run(ctx, dir, "rev-parse", "--verify", "--quiet", rev)
	var ge *gitx.GitError
	if errors.As(err, &ge) && ge.ExitCode == 1 && strings.TrimSpace(string(out)) == "" {
		return "", false, nil
	}
	return strings.TrimSpace(string(out)), err == nil, err
}

// stashTop returns the commit of stash@{0}, or "" when the stash is empty.
func stashTop(ctx context.Context, r gitx.Runner, dir string) (string, error) {
	top, _, err := hasRev(ctx, r, dir, "refs/stash")
	return top, err
}

// stashList returns the commits of git's stash entries, stash@{0} first.
func stashList(ctx context.Context, r gitx.Runner, dir string) ([]string, error) {
	out, err := r.Run(ctx, dir, "stash", "list", "--format=%H")
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

// indexesOf returns the positions of hash in a stash list.
func indexesOf(list []string, hash string) []int {
	var at []int
	for i, h := range list {
		if h == hash {
			at = append(at, i)
		}
	}
	return at
}

func short(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}

func firstFiles(files []string) string {
	if len(files) > 3 {
		return strings.Join(files[:3], ", ") + ", ..."
	}
	return strings.Join(files, ", ")
}

// shelve puts the folder's uncommitted work, untracked files included and
// ignored files left alone, into git's stash and records it; older pending
// shelves of the folder's branch become superseded. It returns false when
// the switch must not go on: Reason is then shelf-failed, or restore-failed
// when the work was stashed and could not be put back.
func (s *switchRun) shelve(st folderStatus) bool {
	switch {
	case len(st.unmerged) > 0:
		s.res.OpResult = refused("shelf-failed", "", capFiles(st.unmerged))
		s.res.Detail = "unmerged files cannot be shelved"
		return false
	case len(st.submodules) > 0:
		s.res.OpResult = refused("shelf-failed", "", capFiles(st.submodules))
		s.res.Detail = "changes inside submodules cannot be shelved; commit or undo them first"
		return false
	}
	sh, why := s.stashPush(st.files)
	if sh != nil {
		err := s.editRecords(func(rec *settings.SwitchRecords) {
			supersede(rec, *sh)
			rec.Shelves = append(rec.Shelves, *sh)
		})
		if err != nil && why == "" {
			why = "could not record the shelf: " + err.Error()
		}
	}
	if why == "" {
		s.res.Shelved = sh
		return true
	}
	s.fail("shelf-failed", why)
	if sh != nil {
		s.res.Shelved = sh
		s.putBack()
	}
	return false
}

// supersede marks the pending shelves of sh's folder and branch: only the
// newest is ever put back on its own.
func supersede(rec *settings.SwitchRecords, sh settings.Shelf) {
	for i := range rec.Shelves {
		o := &rec.Shelves[i]
		if o.State == settings.ShelfPending && o.Branch == sh.Branch && samePath(o.Folder, sh.Folder) {
			o.State, o.WhyCode = settings.ShelfSuperseded, whySuperseded
			o.Why = "a newer shelf of " + sh.Branch + " was taken in this folder (" + short(sh.Hash) + ")"
		}
	}
}

// stashPush runs git stash push and checks that it made one new entry on
// HEAD and left the folder clean. It returns the new shelf (nil when no entry
// was made) and why it did not work out ("" when it did; also when there was
// nothing left to shelve).
func (s *switchRun) stashPush(files []string) (*settings.Shelf, string) {
	dir := s.dir()
	base, err := revParse(s.ctx, s.r, dir, "HEAD")
	if err != nil {
		return nil, "could not read HEAD: " + failDetail(err)
	}
	top, err := stashTop(s.ctx, s.r, dir)
	if err != nil {
		return nil, "could not read the stash: " + failDetail(err)
	}
	t := s.now().Unix()
	cmd := []string{"stash", "push", "--include-untracked", "--message", gitx.ShelfMessage(s.from(), t)}
	entry := settings.OpEntry{
		Time: t, Repo: s.repoRoot, Op: "shelve", Branch: s.from(), Folder: dir,
		Command: append([]string{"git"}, cmd...), Before: base,
	}
	_, runErr := s.r.Run(s.ctx, dir, cmd...)
	after, afterErr := stashTop(s.ctx, s.r, dir)
	var sh *settings.Shelf
	if after != "" && after != top {
		sh = &settings.Shelf{
			Folder: dir, Branch: s.from(), Hash: after, Base: base, Time: t,
			Files: capFiles(files), State: settings.ShelfPending,
		}
		entry.After = after
	}
	if runErr != nil {
		s.addLogError(logRan(s.st, entry, gitFailed(runErr), ""))
		return sh, "git stash push failed: " + failDetail(runErr)
	}
	why, note := s.checkShelved(sh, base), ""
	if afterErr != nil {
		why = "the stash could not be read after git stash push, the work may be in git stash list: " + failDetail(afterErr)
	}
	result := "ok"
	if why != "" {
		result = verifyFailed(why)
	} else if sh == nil {
		note = "no changes were left to shelve"
	}
	s.addLogError(logRan(s.st, entry, result, note))
	return sh, why
}

// checkShelved explains why a stash push did not leave the new entry on
// base and the folder clean, or returns "".
func (s *switchRun) checkShelved(sh *settings.Shelf, base string) string {
	if sh != nil {
		if parent, err := revParse(s.ctx, s.r, s.dir(), sh.Hash+"^1"); err != nil || parent != base {
			return "the new stash entry " + sh.Hash + " is not on HEAD " + base
		}
	}
	st, err := readFolderStatus(s.ctx, s.r, s.dir())
	switch {
	case err != nil:
		return "could not read the folder after git stash push: " + failDetail(err)
	case !st.clean():
		return "the folder still has changes after git stash push: " + firstFiles(st.files)
	}
	return ""
}

// markShelf stores state and reason on the record of hash.
func (s *switchRun) markShelf(hash, state string, r reason) {
	s.editRecords(func(rec *settings.SwitchRecords) {
		for i := range rec.Shelves {
			if rec.Shelves[i].Hash == hash {
				rec.Shelves[i].State, rec.Shelves[i].WhyCode, rec.Shelves[i].Why = state, r.code, r.why
			}
		}
	})
}

// editRecords loads the switch records, lets edit change them and saves
// them. A failure is added to LogError and returned.
func (s *switchRun) editRecords(edit func(*settings.SwitchRecords)) error {
	rec, err := s.st.SwitchRecords(s.repoRoot)
	if err == nil {
		edit(&rec)
		err = s.st.SaveSwitchRecords(s.repoRoot, rec)
	}
	if err != nil {
		s.addLogError("saving the shelf records failed: " + err.Error())
	}
	return err
}

// pendingShelf returns the newest pending shelf the folder left on branch.
func pendingShelf(rec settings.SwitchRecords, folder, branch string) (settings.Shelf, bool) {
	for i := len(rec.Shelves) - 1; i >= 0; i-- {
		sh := rec.Shelves[i]
		if sh.State == settings.ShelfPending && sh.Branch == branch && samePath(sh.Folder, folder) {
			return sh, true
		}
	}
	return settings.Shelf{}, false
}
