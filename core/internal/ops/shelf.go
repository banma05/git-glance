package ops

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/settings"
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

// folderBusy names the operation git has stopped in the folder (a merge,
// rebase, am, cherry-pick, revert or bisect), or returns "".
func folderBusy(ctx context.Context, r gitx.Runner, dir string) (string, error) {
	out, err := r.Run(ctx, dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", err
	}
	gitDir := strings.TrimSpace(string(out))
	for _, s := range []struct{ file, what string }{
		{"MERGE_HEAD", "a merge"}, {"rebase-merge", "a rebase"}, {"rebase-apply", "a rebase or am"},
		{"CHERRY_PICK_HEAD", "a cherry-pick"}, {"REVERT_HEAD", "a revert"}, {"BISECT_START", "a bisect"},
	} {
		_, err := os.Lstat(filepath.Join(gitDir, s.file))
		if err == nil {
			return s.what + " is in progress", nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}
	return "", nil
}

// stashTop returns the commit of stash@{0}, or "" when the stash is empty.
func stashTop(ctx context.Context, r gitx.Runner, dir string) (string, error) {
	out, err := r.Run(ctx, dir, "rev-parse", "--verify", "--quiet", "refs/stash")
	var ge *gitx.GitError
	if errors.As(err, &ge) && ge.ExitCode == 1 && strings.TrimSpace(string(out)) == "" {
		return "", nil
	}
	return strings.TrimSpace(string(out)), err
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
// ignored files left alone, into git's stash and records it. It returns false
// when the switch must not go on: Reason is then shelf-failed, or
// restore-failed when the work was stashed and could not be put back.
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
		if err := s.editRecords(func(rec *settings.SwitchRecords) { rec.Shelves = append(rec.Shelves, *sh) }); err != nil && why == "" {
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
		sh = &settings.Shelf{Folder: dir, Branch: s.from(), Hash: after, Base: base, Time: t, Files: capFiles(files)}
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

// restore puts sh back into the folder when that cannot conflict: the folder
// is on sh's branch at sh.Base and clean. After a clean apply the stash
// entry is dropped. It returns the restored shelf (Why set when its entry
// stays in the stash) or the kept one (Why says why). A kept shelf is marked
// so that it is never applied on its own later when failing is set, or when
// git ran and did not do what it should.
func (s *switchRun) restore(sh settings.Shelf, failing bool) (restored, kept *settings.Shelf) {
	why, err := s.blocksRestore(sh)
	if err != nil {
		why = "could not check the folder: " + failDetail(err)
	}
	if why != "" {
		return nil, s.keep(sh, why, failing)
	}
	list, err := stashList(s.ctx, s.r, s.dir())
	switch {
	case err != nil:
		return nil, s.keep(sh, "could not read git stash list: "+failDetail(err), failing)
	case len(indexesOf(list, sh.Hash)) == 0:
		return nil, s.keep(sh, "it is no longer in git stash list", true)
	}
	if why := s.apply(sh); why != "" {
		return nil, s.keep(sh, why, true)
	}
	return s.drop(sh), nil
}

// blocksRestore explains why applying sh now could conflict, or returns "".
func (s *switchRun) blocksRestore(sh settings.Shelf) (string, error) {
	head, err := headRef(s.ctx, s.r, s.dir())
	if err != nil {
		return "", err
	}
	if head != "refs/heads/"+sh.Branch {
		return fmt.Sprintf("the folder is on %s, not %s", head, sh.Branch), nil
	}
	at, err := revParse(s.ctx, s.r, s.dir(), "HEAD")
	if err != nil {
		return "", err
	}
	if at != sh.Base {
		return fmt.Sprintf("%s moved from %s to %s since the work was shelved; putting it back could conflict",
			sh.Branch, short(sh.Base), short(at)), nil
	}
	st, err := readFolderStatus(s.ctx, s.r, s.dir())
	if err != nil {
		return "", err
	}
	if !st.clean() {
		return "the folder has changes (" + firstFiles(st.files) + "); putting the work back could conflict", nil
	}
	return "", nil
}

// apply runs git stash apply --index and checks that the folder now holds
// exactly the shelf. It returns "" or why the shelf must stay in the stash.
func (s *switchRun) apply(sh settings.Shelf) string {
	cmd := []string{"stash", "apply", "--index", sh.Hash}
	entry := settings.OpEntry{
		Time: s.now().Unix(), Repo: s.repoRoot, Op: "unshelve", Branch: sh.Branch, Folder: s.dir(),
		Command: append([]string{"git"}, cmd...), Before: sh.Hash,
	}
	if _, err := s.r.Run(s.ctx, s.dir(), cmd...); err != nil {
		entry.After, _ = revParse(s.ctx, s.r, s.dir(), "HEAD")
		s.addLogError(logRan(s.st, entry, gitFailed(err), ""))
		return "git stash apply failed: " + failDetail(err)
	}
	entry.After, _ = revParse(s.ctx, s.r, s.dir(), "HEAD")
	if why := s.checkApplied(sh, entry.After); why != "" {
		s.addLogError(logRan(s.st, entry, verifyFailed(why), ""))
		return "git stash apply ran, but " + why
	}
	s.addLogError(logRan(s.st, entry, "ok", ""))
	return ""
}

// checkApplied explains why the folder does not hold exactly the shelf after
// git stash apply, or returns "": HEAD must not have moved, nothing may be
// unmerged, and the index and the tracked files must equal the shelf's.
func (s *switchRun) checkApplied(sh settings.Shelf, head string) string {
	if head != sh.Base {
		return "HEAD is now " + head + ", not " + sh.Base
	}
	st, err := readFolderStatus(s.ctx, s.r, s.dir())
	if err != nil {
		return "the folder could not be read: " + failDetail(err)
	}
	if len(st.unmerged) > 0 {
		return "files are unmerged: " + firstFiles(st.unmerged)
	}
	if why := s.differs("the staged files", "--cached", sh.Hash+"^2"); why != "" {
		return why
	}
	return s.differs("the working files", sh.Hash)
}

// differs runs a quiet diff of the folder against rev and explains a
// difference, or returns "".
func (s *switchRun) differs(what string, against ...string) string {
	args := append([]string{"diff", "--quiet", "--no-ext-diff", "--no-textconv"}, against...)
	_, err := s.r.Run(s.ctx, s.dir(), append(args, "--")...)
	var ge *gitx.GitError
	switch {
	case err == nil:
		return ""
	case errors.As(err, &ge) && ge.ExitCode == 1:
		return what + " differ from the shelf"
	}
	return what + " could not be compared with the shelf: " + failDetail(err)
}

// drop removes the applied shelf's entry from git's stash, found by its
// commit right before the drop, then its record. When the entry is not found
// exactly once, or the drop does not remove it, the work stays applied but
// the entry stays in the stash: the returned shelf says why, and its record
// is marked.
func (s *switchRun) drop(sh settings.Shelf) *settings.Shelf {
	list, err := stashList(s.ctx, s.r, s.dir())
	at := indexesOf(list, sh.Hash)
	switch {
	case err != nil:
		return s.keepApplied(sh, "git stash list could not be read: "+failDetail(err))
	case len(at) == 0:
		return s.keepApplied(sh, "it is no longer in git stash list")
	case len(at) > 1:
		return s.keepApplied(sh, fmt.Sprintf("it is listed %d times in git stash list, so none was dropped", len(at)))
	}
	ref := fmt.Sprintf("stash@{%d}", at[0])
	cmd := []string{"stash", "drop", ref}
	entry := settings.OpEntry{
		Time: s.now().Unix(), Repo: s.repoRoot, Op: "drop-shelf", Branch: sh.Branch, Folder: s.dir(),
		Command: append([]string{"git"}, cmd...), Before: sh.Hash,
	}
	out, err := s.r.Run(s.ctx, s.dir(), cmd...)
	if err != nil {
		s.addLogError(logRan(s.st, entry, gitFailed(err), ""))
		return s.keepApplied(sh, "dropping "+ref+" failed: "+failDetail(err))
	}
	if why := s.checkDropped(sh.Hash, ref, string(out)); why != "" {
		s.addLogError(logRan(s.st, entry, verifyFailed(why), ""))
		return s.keepApplied(sh, why)
	}
	s.addLogError(logRan(s.st, entry, "ok", ""))
	s.editRecords(func(rec *settings.SwitchRecords) {
		kept := rec.Shelves[:0]
		for _, o := range rec.Shelves {
			if o.Hash != sh.Hash {
				kept = append(kept, o)
			}
		}
		rec.Shelves = kept
	})
	return &sh
}

// checkDropped explains why hash is still in git's stash after the drop of
// ref, or returns "". git names the commit it dropped ("Dropped stash@{0}
// (<hash>)"): when that is another entry, which took ours' place in between,
// the answer says how to store it again.
func (s *switchRun) checkDropped(hash, ref, out string) string {
	list, err := stashList(s.ctx, s.r, s.dir())
	if err != nil {
		return "git stash list could not be read after the drop: " + failDetail(err)
	}
	if len(indexesOf(list, hash)) == 0 {
		return ""
	}
	dropped := ""
	if i, j := strings.LastIndex(out, "("), strings.LastIndex(out, ")"); i >= 0 && j > i {
		dropped = out[i+1 : j]
	}
	return fmt.Sprintf("git dropped %s (%s), which was not this shelf; get it back with git stash store %s", ref, dropped, dropped)
}

// keep returns sh with why, marking its record when mark is set.
func (s *switchRun) keep(sh settings.Shelf, why string, mark bool) *settings.Shelf {
	sh.Why = why
	if mark {
		s.markShelf(sh.Hash, why)
	}
	return &sh
}

// keepApplied returns an applied shelf whose stash entry stays, and marks
// its record so that it is not applied twice.
func (s *switchRun) keepApplied(sh settings.Shelf, why string) *settings.Shelf {
	return s.keep(sh, "put back, but its stash entry was kept: "+why, true)
}

func (s *switchRun) markShelf(hash, why string) {
	s.editRecords(func(rec *settings.SwitchRecords) {
		for i := range rec.Shelves {
			if rec.Shelves[i].Hash == hash {
				rec.Shelves[i].Why = why
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

// pendingShelf returns the newest unmarked shelf the folder left on branch.
func pendingShelf(rec settings.SwitchRecords, folder, branch string) (settings.Shelf, bool) {
	for i := len(rec.Shelves) - 1; i >= 0; i-- {
		sh := rec.Shelves[i]
		if sh.Why == "" && sh.Branch == branch && samePath(sh.Folder, folder) {
			return sh, true
		}
	}
	return settings.Shelf{}, false
}

// SwitchState is what a folder can switch back to. Previous is the branch the
// tool last switched the folder away from: "" when there is none, when that
// branch is gone, or when the folder is on it. Shelves are the repository's
// shelves (of any folder) still in git's stash, newest first; Why is set on
// a shelf that will not be put back on its own.
type SwitchState struct {
	Previous string           `json:"previous"`
	Shelves  []settings.Shelf `json:"shelves"`
}

// ReadSwitchState reads the state for folder. It changes nothing.
func ReadSwitchState(ctx context.Context, r gitx.Runner, st settings.Store, repoRoot, folder string) (SwitchState, error) {
	state := SwitchState{Shelves: []settings.Shelf{}}
	rec, err := st.SwitchRecords(repoRoot)
	if err != nil {
		return state, err
	}
	list, err := stashList(ctx, r, repoRoot)
	if err != nil {
		return state, err
	}
	for i := len(rec.Shelves) - 1; i >= 0; i-- {
		if len(indexesOf(list, rec.Shelves[i].Hash)) > 0 {
			state.Shelves = append(state.Shelves, rec.Shelves[i])
		}
	}
	for _, p := range rec.Previous {
		if samePath(p.Folder, folder) {
			state.Previous = p.Branch
		}
	}
	if state.Previous == "" {
		return state, nil
	}
	if _, ok := branchHash(ctx, r, repoRoot, state.Previous); !ok {
		state.Previous = ""
	} else if head, err := headRef(ctx, r, folder); err == nil && head == "refs/heads/"+state.Previous {
		state.Previous = ""
	}
	return state, nil
}
