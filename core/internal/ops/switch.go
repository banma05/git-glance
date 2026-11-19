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
// orphan commits), checked-out-elsewhere (Path is that folder), no-branch
// (no local branch of that name, nor the named remote's branch) and
// in-progress (git stopped a merge, rebase, am, cherry-pick, revert or bisect
// in the folder, Detail says which, or a rebase or bisect works on the
// branch). CreateFolder reasons: bad-name, branch-exists,
// path-inside-folder (Path is the folder) and path-exists; CreateFolderForBranch
// reasons: bad-name, no-branch, checked-out-elsewhere (Path is that folder),
// in-progress (a rebase or bisect works on the branch), path-inside-folder and
// path-exists. Up to here nothing was done. All may also return git-failed
// (git ran and failed; Detail is its message and, for CreateFolder, names a
// branch git created and left behind) and verify-failed (git ran but the
// folder is not on the branch afterwards; Detail says what it is on). Switch
// with shelving adds shelf-failed (the work could not be shelved; nothing was
// switched) and restore-failed (the switch failed and the shelved work could
// not be put back: it is kept in git's stash). LogError is empty unless
// saving settings or the operation log failed.
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

// SwitchOptions are the choices of a switch. Latest first fast-forwards the
// branch to its upstream when it follows the remote branch of its own name
// and has no commits of its own. Shelve puts the folder's uncommitted work
// into git's stash before leaving, and on arrival puts back the work the
// folder left on the branch when that cannot conflict. Remote names the
// remote whose branch becomes a new local branch tracking it when there is
// no local branch of that name; it is ignored when there is one.
type SwitchOptions struct {
	Latest bool
	Shelve bool
	Remote string
}

// SwitchResult is the outcome of Switch. Shelved is the work shelved on the
// way out; Restored is work put back (the arrival branch's shelf, or the
// shelf just taken when the switch failed), with Why set when its stash
// entry could not be dropped; KeptShelf is a shelf that was not put back,
// with Why. FastForwarded says the branch was moved to its upstream, and
// AheadNote ("" or a sentence) that it was not because it has commits of
// its own.
type SwitchResult struct {
	OpResult
	Shelved       *settings.Shelf `json:"shelved"`
	Restored      *settings.Shelf `json:"restored"`
	KeptShelf     *settings.Shelf `json:"keptShelf"`
	FastForwarded bool            `json:"fastForwarded"`
	AheadNote     string          `json:"aheadNote"`
}

// switchPlan is what decideSwitch found. target is the local branch, unless
// remoteRef names the remote-tracking branch to create it from. inPlace means
// the folder is on the branch already and only moves it to its upstream.
type switchPlan struct {
	wt        repo.Worktree
	branch    string
	target    repo.Ref
	remoteRef string
	inPlace   bool
}

// Switch checks branch out in folder. It refuses when the folder is missing,
// detached, stopped in an operation, has hidden changes, or (without
// shelve) has any change or new file; when another folder holds the branch;
// or when there is no local branch of that name and opt.Remote does not name
// a remote branch of it. The decision is made on a snapshot and repeated
// right before git runs; every git command that changes something is logged.
func Switch(ctx context.Context, r gitx.Runner, st settings.Store, repoRoot, folder, branch string, opt SwitchOptions, now func() time.Time) (SwitchResult, error) {
	var p switchPlan
	for pass := 0; pass < 2; pass++ {
		snap, err := repo.Read(ctx, r, repoRoot)
		if err != nil {
			return SwitchResult{OpResult: failed()}, err
		}
		var res OpResult
		var done bool
		p, res, done, err = decideSwitch(snap, folder, branch, opt)
		if err != nil || done {
			return SwitchResult{OpResult: res}, err
		}
	}
	if res, done, err := checkSwitchFolder(ctx, r, repoRoot, p); err != nil || done {
		return SwitchResult{OpResult: res}, err
	}
	s := &switchRun{ctx: ctx, r: r, st: st, repoRoot: repoRoot, now: now, p: p}
	return s.run(opt)
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

// decideSwitch returns the plan. done is true when the answer is already
// final: a refusal, an error, or the folder is on the branch already (and
// opt.Latest is not set).
func decideSwitch(snap repo.Snapshot, folder, branch string, opt SwitchOptions) (switchPlan, OpResult, bool, error) {
	p := switchPlan{branch: branch}
	wt := findWorktree(snap, folder)
	if wt == nil {
		return p, failed(), true, fmt.Errorf("%s is not a folder of this repository", folder)
	}
	p.wt = *wt
	if wt.Missing {
		return p, refused("folder-missing", wt.Path, nil), true, nil
	}
	known := false
	for _, b := range snap.Local {
		if b.Name == branch {
			p.target, known = b, true
		}
	}
	if !known {
		if p.remoteRef = remoteBranch(snap, opt.Remote, branch); p.remoteRef == "" {
			return p, refused("no-branch", "", nil), true, nil
		}
	}
	for _, o := range snap.Worktrees {
		if o.Branch == branch && !o.Detached && !samePath(o.Path, wt.Path) {
			return p, refused("checked-out-elsewhere", o.Path, nil), true, nil
		}
	}
	dirty := wt.Dirty.Changed+wt.Dirty.Untracked > 0
	if dirty && !opt.Shelve {
		return p, refused("dirty", "", capFiles(append([]string{}, wt.Dirty.Files...))), true, nil
	}
	if wt.Detached {
		return p, refused("detached", wt.Path, nil), true, nil
	}
	if wt.Branch == branch {
		return decideInPlace(p, dirty, opt)
	}
	return p, OpResult{}, false, nil
}

// decideInPlace handles a folder that is on the branch already: nothing to
// do, or (latest) move the branch in place. Moving it under uncommitted work
// would leave a shelf that cannot be put back, so a dirty folder is refused.
func decideInPlace(p switchPlan, dirty bool, opt SwitchOptions) (switchPlan, OpResult, bool, error) {
	if !opt.Latest {
		return p, OpResult{OK: true, Files: []string{}, Path: p.wt.Path}, true, nil
	}
	if dirty {
		return p, refused("dirty", "", capFiles(append([]string{}, p.wt.Dirty.Files...))), true, nil
	}
	p.inPlace = true
	return p, OpResult{}, false, nil
}

func findWorktree(snap repo.Snapshot, folder string) *repo.Worktree {
	for i := range snap.Worktrees {
		if samePath(snap.Worktrees[i].Path, folder) {
			return &snap.Worktrees[i]
		}
	}
	return nil
}

// remoteBranch returns refs/remotes/<remote>/<branch> when the snapshot has
// that remote-tracking branch, else "".
func remoteBranch(snap repo.Snapshot, remote, branch string) string {
	if remote == "" {
		return ""
	}
	for _, rr := range snap.Remote {
		if rr.Name == remote+"/"+branch {
			return "refs/remotes/" + rr.Name
		}
	}
	return ""
}

// checkSwitchFolder refuses a folder whose marked tracked files hide changes
// from git status (hidden-changes), and a folder stopped in an operation or
// a branch a rebase or bisect works on (in-progress). done is true when res
// is a refusal.
func checkSwitchFolder(ctx context.Context, r gitx.Runner, repoRoot string, p switchPlan) (OpResult, bool, error) {
	hidden, err := hiddenChanges(ctx, r, p.wt.Path)
	if err != nil {
		return failed(), true, err
	}
	if len(hidden) > 0 {
		return refused("hidden-changes", "", capFiles(hidden)), true, nil
	}
	busy, err := folderBusy(ctx, r, p.wt.Path)
	if err != nil {
		return failed(), true, err
	}
	held, err := heldBranches(ctx, r, repoRoot)
	if err != nil {
		return failed(), true, err
	}
	if busy != "" || held[p.branch] {
		res := refused("in-progress", "", nil)
		res.Detail = busy
		return res, true, nil
	}
	return OpResult{}, false, nil
}

// switchRun carries one switch through its steps; res collects the outcome.
type switchRun struct {
	ctx      context.Context
	r        gitx.Runner
	st       settings.Store
	repoRoot string
	now      func() time.Time
	p        switchPlan
	before   string // the full ref the folder was on
	res      SwitchResult
}

func (s *switchRun) dir() string { return s.p.wt.Path }

func (s *switchRun) from() string { return strings.TrimPrefix(s.before, "refs/heads/") }

func (s *switchRun) addLogError(e string) {
	if e != "" {
		s.res.LogError = joinDetail(s.res.LogError, e)
	}
}

func (s *switchRun) fail(reason, detail string) {
	s.res.Reason, s.res.Detail = reason, detail
}

// run shelves the work when there is any, moves the folder (creating or
// fast-forwarding the branch first when asked), and on arrival puts back the
// branch's shelf. When moving fails after shelving, the work is put back in
// place before returning.
func (s *switchRun) run(opt SwitchOptions) (SwitchResult, error) {
	s.res = SwitchResult{OpResult: OpResult{Files: []string{}, Path: s.dir()}}
	if s.p.inPlace {
		s.res.OK = s.latest()
		return s.res, nil
	}
	var err error
	if s.before, err = headRef(s.ctx, s.r, s.dir()); err != nil {
		return SwitchResult{OpResult: failed()}, err
	}
	if !strings.HasPrefix(s.before, "refs/heads/") {
		return SwitchResult{OpResult: refused("detached", s.dir(), nil)}, nil
	}
	status, err := readFolderStatus(s.ctx, s.r, s.dir())
	if err != nil {
		return SwitchResult{OpResult: failed()}, err
	}
	if !status.clean() {
		if !opt.Shelve {
			return SwitchResult{OpResult: refused("dirty", "", capFiles(status.files))}, nil
		}
		if !s.shelve(status) {
			return s.res, nil
		}
	}
	if !s.move(opt) {
		s.putBack()
		return s.res, nil
	}
	s.res.OK = true
	s.remember()
	s.arrive()
	return s.res, nil
}

func (s *switchRun) move(opt SwitchOptions) bool {
	if s.p.remoteRef != "" {
		cmd := []string{"switch", "--no-guess", "--no-overwrite-ignore", "-c", s.p.branch, "--track", s.p.remoteRef}
		return s.runSwitch(cmd, s.p.remoteRef)
	}
	if opt.Latest && !s.latest() {
		return false
	}
	return s.runSwitch([]string{"switch", "--no-guess", "--no-overwrite-ignore", s.p.branch}, "")
}

// latest fast-forwards the branch to its upstream when it follows the remote
// branch of its own name and has no commits of its own: in the folder when
// it is on the branch, else without touching any folder. A branch with
// commits of its own is left alone and AheadNote says so. It returns false
// when the fast-forward failed (Reason and Detail say why).
func (s *switchRun) latest() bool {
	ff, note := latestPlan(s.p.target)
	s.res.AheadNote = note
	if !ff {
		return true
	}
	var wt *repo.Worktree
	if s.p.inPlace {
		wt = &s.p.wt
	}
	_, skip, logErr, ok := update(s.ctx, s.r, s.st, s.repoRoot, s.p.target, wt, s.now)
	s.addLogError(logErr)
	if !ok {
		s.fail(skip.Reason, skip.Detail)
		return false
	}
	s.res.FastForwarded = true
	return true
}

// latestPlan applies the align rules: only a branch whose upstream is the
// remote branch of its own name, and only when it is behind and not ahead.
func latestPlan(b repo.Ref) (ff bool, note string) {
	if !strings.HasPrefix(b.Upstream, "refs/remotes/") {
		return false, ""
	}
	skip, skipped := checkBranch(b, nil)
	switch {
	case !skipped:
		return b.Behind > 0, ""
	case skip.Reason == "ahead" || skip.Reason == "diverged":
		unit := "commits"
		if b.Ahead == 1 {
			unit = "commit"
		}
		return false, fmt.Sprintf("local has %d %s not on %s; not fast-forwarded", b.Ahead, unit, strings.TrimPrefix(b.Upstream, "refs/remotes/"))
	}
	return false, ""
}

// runSwitch runs a switch command, checks that the folder is on the branch
// afterwards (and, for a branch created from upstream, that the branch is at
// and tracks upstream) and logs it whatever the outcome.
func (s *switchRun) runSwitch(cmd []string, upstream string) bool {
	branchRef := "refs/heads/" + s.p.branch
	entry := settings.OpEntry{
		Time: s.now().Unix(), Repo: s.repoRoot, Op: "switch", Branch: s.p.branch, Folder: s.dir(),
		Command: append([]string{"git"}, cmd...), Before: s.before,
	}
	if _, err := s.r.Run(s.ctx, s.dir(), cmd...); err != nil {
		entry.After, _ = headRef(s.ctx, s.r, s.dir())
		s.fail("git-failed", failDetail(err))
		s.addLogError(logRan(s.st, entry, gitFailed(err), ""))
		return false
	}
	head, why := checkOnBranch(s.ctx, s.r, s.dir(), branchRef)
	entry.After = head
	if why == "" && upstream != "" {
		why = s.checkTracks(branchRef, upstream)
	}
	if why != "" {
		s.fail("verify-failed", why)
		s.addLogError(logRan(s.st, entry, verifyFailed(why), ""))
		return false
	}
	s.addLogError(logRan(s.st, entry, "ok", ""))
	return true
}

// checkTracks explains why a newly created branch is not at upstream or does
// not track it, or returns "".
func (s *switchRun) checkTracks(branchRef, upstream string) string {
	out, err := s.r.Run(s.ctx, s.repoRoot, "for-each-ref", "--format=%(upstream)", branchRef)
	if got := strings.TrimSpace(string(out)); err != nil || got != upstream {
		return fmt.Sprintf("%s does not track %s (it tracks %q)", branchRef, upstream, got)
	}
	at, err1 := revParse(s.ctx, s.r, s.repoRoot, branchRef)
	want, err2 := revParse(s.ctx, s.r, s.repoRoot, upstream)
	if err1 != nil || err2 != nil || at != want {
		return fmt.Sprintf("%s is at %s, %s at %s", branchRef, at, upstream, want)
	}
	return ""
}

// remember records the branch the folder came from, for switching back.
func (s *switchRun) remember() {
	s.editRecords(func(rec *settings.SwitchRecords) {
		for i := range rec.Previous {
			if samePath(rec.Previous[i].Folder, s.dir()) {
				rec.Previous[i].Branch = s.from()
				return
			}
		}
		rec.Previous = append(rec.Previous, settings.PreviousBranch{Folder: s.dir(), Branch: s.from()})
	})
}

// arrive puts back the newest shelf the folder left on this branch, when that
// cannot conflict; otherwise KeptShelf says why it stays in git's stash.
func (s *switchRun) arrive() {
	rec, err := s.st.SwitchRecords(s.repoRoot)
	if err != nil {
		s.addLogError("reading the shelf records failed: " + err.Error())
		return
	}
	if sh, ok := pendingShelf(rec, s.dir(), s.p.branch); ok {
		s.res.Restored, s.res.KeptShelf = s.restore(sh, false)
	}
}

// putBack returns the work shelved on the way out after moving failed, while
// the folder is still where it started. When that is not possible the shelf
// is kept and marked, and Reason becomes restore-failed.
func (s *switchRun) putBack() {
	if s.res.Shelved == nil {
		return
	}
	restored, kept := s.restore(*s.res.Shelved, true)
	s.res.Restored = restored
	if kept != nil {
		s.res.KeptShelf = kept
		s.fail("restore-failed", joinDetail(s.res.Detail,
			"the shelved work was not put back ("+kept.Why+"); it is kept in git stash list as "+kept.Hash))
	}
}
