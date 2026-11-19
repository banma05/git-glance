package ops

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/settings"
)

// reason is why a shelf stays in the stash: a Shelf.WhyCode and its English
// sentence. The zero reason means none.
type reason struct{ code, why string }

// restore puts sh back into the folder when that cannot conflict: the folder
// is on sh's branch at sh.Base, clean, and nothing is on disk where the shelf
// would create a file. After a verified apply the stash entry is dropped. It
// returns the restored shelf or the kept one, with state and reason. A kept
// shelf stays pending when only those conditions were not met and failing is
// not set; otherwise it is marked kept and never applied on its own again.
func (s *switchRun) restore(sh settings.Shelf, failing bool) (restored, kept *settings.Shelf) {
	r, err := s.blocksRestore(sh)
	if err != nil {
		r = reason{whyCheckFailed, "the folder could not be checked: " + failDetail(err)}
	}
	if r.code != "" {
		return nil, s.keep(sh, r, failing)
	}
	list, err := stashList(s.ctx, s.r, s.dir())
	switch {
	case err != nil:
		return nil, s.keep(sh, reason{whyCheckFailed, "git stash list could not be read: " + failDetail(err)}, failing)
	case len(indexesOf(list, sh.Hash)) == 0:
		return nil, s.keep(sh, reason{whyEntryGone, "it is no longer in git stash list"}, true)
	}
	if r := s.apply(sh); r.code != "" {
		return nil, s.keep(sh, r, true)
	}
	return s.drop(sh), nil
}

// blocksRestore explains why applying sh now could conflict, or returns the
// zero reason.
func (s *switchRun) blocksRestore(sh settings.Shelf) (reason, error) {
	head, err := headRef(s.ctx, s.r, s.dir())
	if err != nil {
		return reason{}, err
	}
	if head != "refs/heads/"+sh.Branch {
		return reason{whyNotOnBranch, fmt.Sprintf("the folder is on %s, not %s", head, sh.Branch)}, nil
	}
	at, err := revParse(s.ctx, s.r, s.dir(), "HEAD")
	if err != nil {
		return reason{}, err
	}
	if at != sh.Base {
		return reason{whyBaseMoved, fmt.Sprintf("%s moved from %s to %s since the work was shelved; putting it back could conflict",
			sh.Branch, short(sh.Base), short(at))}, nil
	}
	st, err := readFolderStatus(s.ctx, s.r, s.dir())
	if err != nil {
		return reason{}, err
	}
	if !st.clean() {
		return reason{whyFolderDirty, "the folder has changes (" + firstFiles(st.files) + "); putting the work back could conflict"}, nil
	}
	taken, err := s.takenPaths(sh)
	if err != nil {
		return reason{}, err
	}
	if len(taken) > 0 {
		return reason{whyPathExists, "files the shelf would create are already on disk (" + firstFiles(taken) +
			"); putting the work back would stop on them or overwrite them"}, nil
	}
	return reason{}, nil
}

// takenPaths lists the paths sh would create that something on disk already
// takes, ignored files included: git status does not show those, and git
// stash apply stops half way on them or overwrites them.
func (s *switchRun) takenPaths(sh settings.Shelf) ([]string, error) {
	paths, err := s.shelfNewPaths(sh)
	if err != nil {
		return nil, err
	}
	var taken []string
	for _, p := range paths {
		if pathTaken(s.dir(), p) {
			taken = append(taken, p)
		}
	}
	return taken, nil
}

// shelfNewPaths lists the paths sh adds to its base: the files it adds to the
// index or the working tree, and its untracked files (its third parent).
func (s *switchRun) shelfNewPaths(sh settings.Shelf) ([]string, error) {
	seen := map[string]bool{}
	var paths []string
	add := func(out []byte) {
		for _, p := range strings.Split(string(out), "\x00") {
			if p != "" && !seen[p] {
				seen[p] = true
				paths = append(paths, p)
			}
		}
	}
	for _, rev := range []string{sh.Hash, sh.Hash + "^2"} {
		out, err := s.r.Run(s.ctx, s.dir(), "diff", "--name-only", "--no-renames", "--no-relative", "--diff-filter=A",
			"-z", "--no-ext-diff", "--no-textconv", sh.Base, rev, "--")
		if err != nil {
			return nil, err
		}
		add(out)
	}
	untracked := sh.Hash + "^3"
	if _, ok, err := hasRev(s.ctx, s.r, s.dir(), untracked+"^{commit}"); err != nil || !ok {
		return paths, err
	}
	out, err := s.r.Run(s.ctx, s.dir(), "ls-tree", "-r", "--name-only", "--full-tree", "-z", untracked)
	if err != nil {
		return nil, err
	}
	add(out)
	return paths, nil
}

// pathTaken reports whether writing rel under dir would meet something: the
// path exists, or one of its parent folders is a file or a link. A path that
// cannot be checked counts as taken.
func pathTaken(dir, rel string) bool {
	p := dir
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		p = filepath.Join(p, part)
		info, err := os.Lstat(p)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return false
		case err != nil || i == len(parts)-1:
			return true
		case !info.IsDir():
			return true
		}
	}
	return false
}

// apply runs git stash apply --index and checks that the folder now holds
// exactly the shelf. It returns the zero reason, or why the shelf must stay
// in the stash, saying whether part of the work may now be in the folder.
func (s *switchRun) apply(sh settings.Shelf) reason {
	cmd := []string{"stash", "apply", "--index", sh.Hash}
	entry := settings.OpEntry{
		Time: s.now().Unix(), Repo: s.repoRoot, Op: "unshelve", Branch: sh.Branch, Folder: s.dir(),
		Command: append([]string{"git"}, cmd...), Before: sh.Hash,
	}
	if _, err := s.r.Run(s.ctx, s.dir(), cmd...); err != nil {
		entry.After, _ = revParse(s.ctx, s.r, s.dir(), "HEAD")
		s.addLogError(logRan(s.st, entry, gitFailed(err), ""))
		return s.applyFailed(sh, "git stash apply failed: "+failDetail(err))
	}
	entry.After, _ = revParse(s.ctx, s.r, s.dir(), "HEAD")
	if why := s.checkApplied(sh, entry.After); why != "" {
		s.addLogError(logRan(s.st, entry, verifyFailed(why), ""))
		return reason{whyApplyMismatch, "git stash apply ran, but " + why +
			"; part or all of the work may now be in the folder, and all of it is still in git stash list as " + sh.Hash}
	}
	s.addLogError(logRan(s.st, entry, "ok", ""))
	return reason{}
}

// applyFailed says whether a failed apply left the folder clean or holding
// part of the work.
func (s *switchRun) applyFailed(sh settings.Shelf, what string) reason {
	st, err := readFolderStatus(s.ctx, s.r, s.dir())
	if err == nil && st.clean() {
		return reason{whyApplyFailed, what + "; nothing was changed, and the work is in git stash list as " + sh.Hash}
	}
	files := "the folder could not be read"
	if err == nil {
		files = firstFiles(st.files)
	}
	return reason{whyApplyPartial, what + "; part of the work may now be in the folder (" + files +
		"), and all of it is still in git stash list as " + sh.Hash}
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
// exactly once, or the drop does not remove it, the work stays applied and
// the entry stays in the stash: the returned shelf says why, and its record
// is marked. An entry git dropped instead of ours is stored again at once.
func (s *switchRun) drop(sh settings.Shelf) *settings.Shelf {
	list, err := stashList(s.ctx, s.r, s.dir())
	at := indexesOf(list, sh.Hash)
	switch {
	case err != nil:
		return s.keepApplied(sh, reason{whyCheckFailed, "git stash list could not be read: " + failDetail(err)})
	case len(at) == 0:
		return s.keepApplied(sh, reason{whyEntryGone, "it is no longer in git stash list"})
	case len(at) > 1:
		return s.keepApplied(sh, reason{whyEntryAmbiguous, fmt.Sprintf("it is listed %d times in git stash list, so none was dropped", len(at))})
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
		return s.keepApplied(sh, reason{whyDropFailed, "dropping " + ref + " failed: " + failDetail(err)})
	}
	r, dropped := s.checkDropped(sh.Hash, ref, string(out))
	if r.code != "" {
		s.addLogError(logRan(s.st, entry, verifyFailed(r.why), ""))
		if r.code == whyDropShifted {
			r.why += "; " + s.storeAgain(sh.Branch, dropped)
		}
		return s.keepApplied(sh, r)
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
	sh.State, sh.WhyCode, sh.Why = settings.ShelfRestored, "", ""
	return &sh
}

// checkDropped explains why hash is still in git's stash after the drop of
// ref, or returns the zero reason. git names the commit it dropped ("Dropped
// stash@{0} (<hash>)"); when that is another entry, which took ours' place
// in between, it is returned.
func (s *switchRun) checkDropped(hash, ref, out string) (reason, string) {
	list, err := stashList(s.ctx, s.r, s.dir())
	if err != nil {
		return reason{whyCheckFailed, "git stash list could not be read after the drop: " + failDetail(err)}, ""
	}
	if len(indexesOf(list, hash)) == 0 {
		return reason{}, ""
	}
	dropped := ""
	if i, j := strings.LastIndex(out, "("), strings.LastIndex(out, ")"); i >= 0 && j > i {
		dropped = out[i+1 : j]
	}
	return reason{whyDropShifted, fmt.Sprintf("git dropped %s (%s), which was not this shelf", ref, dropped)}, dropped
}

// storeAgain puts an entry that git dropped by mistake back into the stash,
// as stash@{0} with its message, and says what happened.
func (s *switchRun) storeAgain(branch, hash string) string {
	if !isHash(hash) {
		return "git did not name the entry it dropped; look for it with git fsck --unreachable"
	}
	out, _ := s.r.Run(s.ctx, s.dir(), "log", "--no-show-signature", "-1", "--format=%s", hash, "--")
	cmd := []string{"stash", "store", "--message", gitx.StoreMessage(strings.TrimSpace(string(out))), hash}
	entry := settings.OpEntry{
		Time: s.now().Unix(), Repo: s.repoRoot, Op: "store-entry", Branch: branch, Folder: s.dir(),
		Command: append([]string{"git"}, cmd...), After: hash,
	}
	if _, err := s.r.Run(s.ctx, s.dir(), cmd...); err != nil {
		s.addLogError(logRan(s.st, entry, gitFailed(err), ""))
		return "storing it again failed (" + failDetail(err) + "); get it back with git stash store " + hash
	}
	if top, err := stashTop(s.ctx, s.r, s.dir()); err != nil || top != hash {
		s.addLogError(logRan(s.st, entry, verifyFailed("stash@{0} is not "+hash+" after git stash store"), ""))
		return "storing it again could not be confirmed; if it is missing from git stash list, get it back with git stash store " + hash
	}
	s.addLogError(logRan(s.st, entry, "ok", ""))
	return "it was stored again as stash@{0}"
}

// keep returns sh with reason r: pending, or kept (and marked so) when mark
// is set.
func (s *switchRun) keep(sh settings.Shelf, r reason, mark bool) *settings.Shelf {
	sh.State, sh.WhyCode, sh.Why = settings.ShelfPending, r.code, r.why
	if mark {
		sh.State = settings.ShelfKept
		s.markShelf(sh.Hash, sh.State, r)
	}
	return &sh
}

// keepApplied returns an applied shelf whose stash entry stays, and marks
// its record so that it is not applied twice.
func (s *switchRun) keepApplied(sh settings.Shelf, r reason) *settings.Shelf {
	r.why = "put back, but its stash entry was kept: " + r.why
	sh.State, sh.WhyCode, sh.Why = settings.ShelfRestoredEntryKept, r.code, r.why
	s.markShelf(sh.Hash, sh.State, r)
	return &sh
}
