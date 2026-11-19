package ops

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/settings"
)

func (f *fixture) write(t *testing.T, rel, content string) {
	t.Helper()
	p := filepath.Join(f.r.Dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// read returns the content of a file of the main folder, or "<absent>".
func (f *fixture) read(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.r.Dir, filepath.FromSlash(rel)))
	if os.IsNotExist(err) {
		return "<absent>"
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (f *fixture) exclude(t *testing.T, patterns string) {
	t.Helper()
	p := filepath.Join(f.r.Dir, ".git", "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(patterns), 0o644); err != nil {
		t.Fatal(err)
	}
}

// stashList returns the commit hashes of git's stash, newest first.
func (f *fixture) stashList() []string {
	out := f.r.Git(f.r.Dir, "stash", "list", "--format=%H")
	if out == "" {
		return []string{}
	}
	return strings.Split(out, "\n")
}

func (f *fixture) status() string {
	return f.r.Git(f.r.Dir, "status", "--porcelain", "--untracked-files=all")
}

// sw switches the main folder with the real runner.
func (f *fixture) sw(t *testing.T, branch string, opt SwitchOptions) SwitchResult {
	t.Helper()
	return f.swWith(t, f.run, branch, opt)
}

func (f *fixture) swWith(t *testing.T, r gitx.Runner, branch string, opt SwitchOptions) SwitchResult {
	t.Helper()
	res, err := Switch(context.Background(), r, f.st, f.r.Dir, f.r.Dir, branch, opt, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if res.Files == nil {
		t.Fatalf("res.Files is nil: %+v", res)
	}
	return res
}

func (f *fixture) state(t *testing.T) SwitchState {
	t.Helper()
	s, err := ReadSwitchState(context.Background(), f.run, f.st, f.r.Dir, f.r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Shelves == nil {
		t.Fatal("state.Shelves is nil")
	}
	return s
}

// onLogin puts the main folder on a new branch feature/login with one commit
// of its own.
func (f *fixture) onLogin(t *testing.T) string {
	t.Helper()
	f.r.Git(f.r.Dir, "switch", "-q", "-c", "feature/login")
	return f.r.Commit(f.r.Dir, "login", map[string]string{"login.txt": "login\n"})
}

// opsOf returns the op-log entries of the repository, oldest first.
func (f *fixture) opsOf(t *testing.T) []settings.OpEntry {
	t.Helper()
	ops, err := f.st.Ops(f.r.Dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(ops)
	return ops
}

func opSummary(ops []settings.OpEntry) []string {
	out := []string{}
	for _, o := range ops {
		out = append(out, o.Op+" "+o.Branch+" "+o.Result)
	}
	return out
}

func wantOps(t *testing.T, f *fixture, want ...string) []settings.OpEntry {
	t.Helper()
	ops := f.opsOf(t)
	if got := opSummary(ops); !slices.Equal(got, want) {
		t.Fatalf("ops =\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
	return ops
}

// failRunner fails the commands matched by fail without running them.
type failRunner struct {
	gitx.ExecRunner
	fail func(args []string) bool
}

func (f failRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if f.fail(args) {
		return nil, &gitx.GitError{Args: args, ExitCode: 1, Stderr: "error: simulated failure\n"}
	}
	return f.ExecRunner.Run(ctx, dir, args...)
}

func isStash(args []string, sub string) bool {
	return len(args) > 1 && args[0] == "stash" && args[1] == sub
}

// Dirty work on feature/login is shelved on the way to main and comes back,
// staged and unstaged parts and new files alike, on the way back; the stash
// entry is then dropped.
func TestSwitchShelvesAndRestores(t *testing.T) {
	f := newFixture(t)
	base := f.onLogin(t)
	f.write(t, "README", "edited\n")
	f.write(t, "login.txt", "staged\n")
	f.r.Git(f.r.Dir, "add", "login.txt")
	f.write(t, "login.txt", "staged, then edited\n")
	f.write(t, "notes/new.txt", "new\n")

	res := f.sw(t, "main", SwitchOptions{Shelve: true})
	if !res.OK || res.Shelved == nil || res.Restored != nil || res.KeptShelf != nil || res.LogError != "" {
		t.Fatalf("away: res = %+v", res)
	}
	sh := *res.Shelved
	if sh.Branch != "feature/login" || !samePath(sh.Folder, f.r.Dir) || sh.Base != base || !isHash(sh.Hash) ||
		sh.Time != fixedNow().Unix() || sh.Why != "" {
		t.Fatalf("shelf = %+v", sh)
	}
	slices.Sort(sh.Files)
	if !slices.Equal(sh.Files, []string{"README", "login.txt", "notes/new.txt"}) {
		t.Fatalf("shelf files = %v", sh.Files)
	}
	if f.head(f.r.Dir) != "refs/heads/main" || f.status() != "" || f.read(t, "notes/new.txt") != "<absent>" {
		t.Fatalf("folder after shelving: head %s, status %q", f.head(f.r.Dir), f.status())
	}
	if got := f.stashList(); !slices.Equal(got, []string{sh.Hash}) {
		t.Fatalf("stash list = %v", got)
	}
	if msg := f.r.Git(f.r.Dir, "log", "-1", "--format=%s", sh.Hash); msg != "On feature/login: git-glance shelf feature/login 1700000000" {
		t.Fatalf("stash message = %q", msg)
	}
	if st := f.state(t); st.Previous != "feature/login" || len(st.Shelves) != 1 || st.Shelves[0].Hash != sh.Hash {
		t.Fatalf("state away = %+v", st)
	}

	back := f.sw(t, "feature/login", SwitchOptions{Shelve: true})
	if !back.OK || back.Shelved != nil || back.Restored == nil || back.KeptShelf != nil || back.LogError != "" {
		t.Fatalf("back: res = %+v", back)
	}
	if back.Restored.Hash != sh.Hash || back.Restored.Why != "" {
		t.Fatalf("restored = %+v", back.Restored)
	}
	if f.read(t, "README") != "edited\n" || f.read(t, "login.txt") != "staged, then edited\n" || f.read(t, "notes/new.txt") != "new\n" {
		t.Fatal("the work did not come back")
	}
	if staged := f.r.Git(f.r.Dir, "show", ":login.txt"); staged != "staged" {
		t.Fatalf("staged login.txt = %q, want the staged version back", staged)
	}
	if got := f.stashList(); len(got) != 0 {
		t.Fatalf("stash list = %v, want our entry dropped", got)
	}
	if st := f.state(t); st.Previous != "main" || len(st.Shelves) != 0 {
		t.Fatalf("state back = %+v", st)
	}
	ops := wantOps(t, f,
		"shelve feature/login ok", "switch main ok",
		"switch feature/login ok", "unshelve feature/login ok", "drop-shelf feature/login ok")
	if ops[0].Before != base || ops[0].After != sh.Hash ||
		strings.Join(ops[0].Command, " ") != "git stash push --include-untracked --message git-glance shelf feature/login 1700000000" {
		t.Fatalf("shelve entry = %+v", ops[0])
	}
	if ops[3].Before != sh.Hash || ops[3].After != base || strings.Join(ops[3].Command, " ") != "git stash apply --index "+sh.Hash {
		t.Fatalf("unshelve entry = %+v", ops[3])
	}
	if ops[4].Before != sh.Hash || strings.Join(ops[4].Command, " ") != "git stash drop stash@{0}" {
		t.Fatalf("drop entry = %+v", ops[4])
	}
}

// Untracked files are shelved and come back; ignored files, also those
// inside an untracked folder, stay where they are and are never shelved.
func TestShelfKeepsUntrackedSkipsIgnored(t *testing.T) {
	f := newFixture(t)
	f.onLogin(t)
	f.exclude(t, "*.log\nbuild/\n")
	f.write(t, "build/out.bin", "ignored build\n")
	f.write(t, "app.log", "ignored log\n")
	f.write(t, "newdir/u.txt", "untracked\n")
	f.write(t, "newdir/x.log", "ignored inside\n")

	res := f.sw(t, "main", SwitchOptions{Shelve: true})
	if !res.OK || res.Shelved == nil {
		t.Fatalf("res = %+v", res)
	}
	if !slices.Equal(res.Shelved.Files, []string{"newdir/u.txt"}) {
		t.Fatalf("shelf files = %v", res.Shelved.Files)
	}
	if got := f.r.Git(f.r.Dir, "show", "--name-only", "--format=", res.Shelved.Hash+"^3"); got != "newdir/u.txt" {
		t.Fatalf("untracked files in the stash = %q", got)
	}
	for rel, want := range map[string]string{
		"build/out.bin": "ignored build\n", "app.log": "ignored log\n", "newdir/x.log": "ignored inside\n", "newdir/u.txt": "<absent>",
	} {
		if got := f.read(t, rel); got != want {
			t.Fatalf("on main, %s = %q, want %q", rel, got, want)
		}
	}
	back := f.sw(t, "feature/login", SwitchOptions{Shelve: true})
	if !back.OK || back.Restored == nil {
		t.Fatalf("back = %+v", back)
	}
	for rel, want := range map[string]string{
		"build/out.bin": "ignored build\n", "app.log": "ignored log\n", "newdir/x.log": "ignored inside\n", "newdir/u.txt": "untracked\n",
	} {
		if got := f.read(t, rel); got != want {
			t.Fatalf("back, %s = %q, want %q", rel, got, want)
		}
	}
}

// The shelved branch moved while the folder was away: putting the work back
// could conflict, so nothing is applied and the shelf stays in the stash.
func TestSwitchBackKeepsShelfWhenBaseMoved(t *testing.T) {
	f := newFixture(t)
	f.onLogin(t)
	f.write(t, "README", "edited\n")
	res := f.sw(t, "main", SwitchOptions{Shelve: true})
	if !res.OK || res.Shelved == nil {
		t.Fatalf("res = %+v", res)
	}
	hash := res.Shelved.Hash
	wt := f.r.AddWorktree("login", "feature/login")
	moved := f.r.Commit(wt, "more", map[string]string{"README": "theirs\n"})
	f.r.Git(f.r.Dir, "worktree", "remove", wt)

	back := f.sw(t, "feature/login", SwitchOptions{Shelve: true})
	if !back.OK || back.Restored != nil || back.KeptShelf == nil {
		t.Fatalf("back = %+v", back)
	}
	if back.KeptShelf.Hash != hash || !strings.Contains(back.KeptShelf.Why, "moved") || !strings.Contains(back.KeptShelf.Why, moved[:12]) {
		t.Fatalf("kept = %+v", back.KeptShelf)
	}
	if f.read(t, "README") != "theirs\n" || f.status() != "" {
		t.Fatalf("the folder changed: README %q, status %q", f.read(t, "README"), f.status())
	}
	if got := f.stashList(); !slices.Equal(got, []string{hash}) {
		t.Fatalf("stash list = %v", got)
	}
	st := f.state(t)
	if len(st.Shelves) != 1 || st.Shelves[0].Hash != hash {
		t.Fatalf("state = %+v", st)
	}
	wantOps(t, f, "shelve feature/login ok", "switch main ok", "switch feature/login ok")
}

// The folder is not clean on arrival (a file came along with the switch):
// nothing is applied.
func TestSwitchBackKeepsShelfWhenFolderNotClean(t *testing.T) {
	f := newFixture(t)
	f.onLogin(t)
	f.write(t, "README", "edited\n")
	res := f.sw(t, "main", SwitchOptions{Shelve: true})
	if !res.OK || res.Shelved == nil {
		t.Fatalf("res = %+v", res)
	}
	late := hookRunner{before: func(dir string, args []string) {
		if args[0] == "switch" {
			f.write(t, "late.txt", "came along\n")
		}
	}}
	back := f.swWith(t, late, "feature/login", SwitchOptions{Shelve: true})
	if !back.OK || back.Restored != nil || back.KeptShelf == nil || !strings.Contains(back.KeptShelf.Why, "late.txt") {
		t.Fatalf("back = %+v", back)
	}
	if f.read(t, "README") != "init\n" || f.read(t, "late.txt") != "came along\n" {
		t.Fatal("the folder changed")
	}
	if got := f.stashList(); !slices.Equal(got, []string{res.Shelved.Hash}) {
		t.Fatalf("stash list = %v", got)
	}
}

// branchWithIgnoredBlocker makes main track gen.txt and puts the main folder
// on feature/login, where gen.txt is ignored and present: git refuses to
// switch to main over it, after the shelf was taken.
func branchWithIgnoredBlocker(t *testing.T, f *fixture) {
	t.Helper()
	f.r.Commit(f.r.Dir, "gen on main", map[string]string{"gen.txt": "tracked\n"})
	f.r.Git(f.r.Dir, "switch", "-q", "-c", "feature/login", "HEAD~1")
	f.r.Commit(f.r.Dir, "login", map[string]string{"login.txt": "login\n"})
	f.exclude(t, "gen.txt\n")
	f.write(t, "gen.txt", "mine\n")
	f.write(t, "README", "edited\n")
	f.write(t, "new.txt", "new\n")
}

// The switch fails after the shelf was taken: the work is put back in place
// at once and the result says the switch failed and the work is back.
func TestSwitchFailureRestoresShelfInPlace(t *testing.T) {
	f := newFixture(t)
	branchWithIgnoredBlocker(t, f)
	res := f.sw(t, "main", SwitchOptions{Shelve: true})
	if res.OK || res.Reason != "git-failed" || res.Detail == "" || res.Shelved == nil || res.Restored == nil || res.KeptShelf != nil {
		t.Fatalf("res = %+v", res)
	}
	if res.Restored.Hash != res.Shelved.Hash || res.Restored.Why != "" {
		t.Fatalf("restored = %+v", res.Restored)
	}
	if f.head(f.r.Dir) != "refs/heads/feature/login" || f.read(t, "README") != "edited\n" ||
		f.read(t, "new.txt") != "new\n" || f.read(t, "gen.txt") != "mine\n" {
		t.Fatal("the folder is not as it was")
	}
	if got := f.stashList(); len(got) != 0 {
		t.Fatalf("stash list = %v", got)
	}
	if st := f.state(t); st.Previous != "" || len(st.Shelves) != 0 {
		t.Fatalf("state = %+v", st)
	}
	ops := wantOps(t, f, "shelve feature/login ok", "switch main "+f.opsOf(t)[1].Result,
		"unshelve feature/login ok", "drop-shelf feature/login ok")
	if !strings.HasPrefix(ops[1].Result, "git-failed: ") {
		t.Fatalf("switch entry = %+v", ops[1])
	}
}

// The switch fails and putting the work back is not possible either: the
// shelf is kept, marked, and the result says so.
func TestSwitchFailureKeepsShelfWhenPutBackFails(t *testing.T) {
	f := newFixture(t)
	branchWithIgnoredBlocker(t, f)
	late := hookRunner{before: func(dir string, args []string) {
		if args[0] == "switch" {
			f.write(t, "late.txt", "written meanwhile\n")
		}
	}}
	res := f.swWith(t, late, "main", SwitchOptions{Shelve: true})
	if res.OK || res.Reason != "restore-failed" || res.Restored != nil || res.KeptShelf == nil || res.Shelved == nil {
		t.Fatalf("res = %+v", res)
	}
	if res.KeptShelf.Hash != res.Shelved.Hash || res.KeptShelf.Why == "" || !strings.Contains(res.Detail, "git stash") {
		t.Fatalf("kept = %+v, detail %q", res.KeptShelf, res.Detail)
	}
	if got := f.stashList(); !slices.Equal(got, []string{res.Shelved.Hash}) {
		t.Fatalf("stash list = %v", got)
	}
	st := f.state(t)
	if len(st.Shelves) != 1 || st.Shelves[0].Why == "" {
		t.Fatalf("state = %+v, want the kept shelf listed with why", st)
	}
	// A kept shelf is never applied on its own later: the user decides.
	if err := os.Remove(filepath.Join(f.r.Dir, "late.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(f.r.Dir, "gen.txt")); err != nil {
		t.Fatal(err)
	}
	f.sw(t, "main", SwitchOptions{Shelve: true})
	again := f.sw(t, "feature/login", SwitchOptions{Shelve: true})
	if !again.OK || again.Restored != nil || f.read(t, "README") != "init\n" {
		t.Fatalf("again = %+v", again)
	}
}

// The user stashes work of their own while away: dropping our shelf hits
// exactly our entry, never theirs.
func TestDropFindsOwnShelfAmongOthers(t *testing.T) {
	f := newFixture(t)
	f.onLogin(t)
	f.write(t, "README", "edited\n")
	res := f.sw(t, "main", SwitchOptions{Shelve: true})
	if !res.OK || res.Shelved == nil {
		t.Fatalf("res = %+v", res)
	}
	f.write(t, "README", "mine 1\n")
	f.r.Git(f.r.Dir, "stash", "push", "-q", "-m", "mine 1")
	f.write(t, "README", "mine 2\n")
	f.r.Git(f.r.Dir, "stash", "push", "-q", "-m", "mine 2")
	all := f.stashList()
	if len(all) != 3 || all[2] != res.Shelved.Hash {
		t.Fatalf("stash list = %v", all)
	}

	back := f.sw(t, "feature/login", SwitchOptions{Shelve: true})
	if !back.OK || back.Restored == nil || back.Restored.Why != "" || f.read(t, "README") != "edited\n" {
		t.Fatalf("back = %+v", back)
	}
	if got := f.stashList(); !slices.Equal(got, all[:2]) {
		t.Fatalf("stash list = %v, want theirs %v", got, all[:2])
	}
	ops := f.opsOf(t)
	if last := ops[len(ops)-1]; strings.Join(last.Command, " ") != "git stash drop stash@{2}" || last.Result != "ok" {
		t.Fatalf("drop entry = %+v", last)
	}
}

// Our entry is listed twice (stored again by hand): which one is ours is not
// certain, so the work is put back but nothing is dropped.
func TestDropKeepsAmbiguousShelf(t *testing.T) {
	f := newFixture(t)
	f.onLogin(t)
	f.write(t, "README", "edited\n")
	res := f.sw(t, "main", SwitchOptions{Shelve: true})
	hash := res.Shelved.Hash
	// git adds no entry when the stash already points at the commit, so
	// another entry goes in between.
	f.write(t, "README", "mine\n")
	f.r.Git(f.r.Dir, "stash", "push", "-q", "-m", "mine")
	f.r.Git(f.r.Dir, "stash", "store", "-m", "copy", hash)
	list := f.stashList()
	if len(list) != 3 || list[0] != hash || list[2] != hash {
		t.Fatalf("stash list = %v", list)
	}

	back := f.sw(t, "feature/login", SwitchOptions{Shelve: true})
	if !back.OK || back.Restored == nil || !strings.Contains(back.Restored.Why, "listed 2 times") || back.KeptShelf != nil {
		t.Fatalf("back = %+v", back)
	}
	if f.read(t, "README") != "edited\n" {
		t.Fatal("the work did not come back")
	}
	if got := f.stashList(); !slices.Equal(got, list) {
		t.Fatalf("stash list = %v, want %v", got, list)
	}
	if st := f.state(t); len(st.Shelves) != 1 || st.Shelves[0].Why == "" {
		t.Fatalf("state = %+v", st)
	}
	wantOps(t, f, "shelve feature/login ok", "switch main ok", "switch feature/login ok", "unshelve feature/login ok")
}

// Someone stashes between our look at the list and the drop: the entry git
// dropped is not ours, and the result names it so it can be stored again.
func TestDropReportsAnEntryThatShiftedUnderIt(t *testing.T) {
	f := newFixture(t)
	f.onLogin(t)
	f.write(t, "README", "edited\n")
	res := f.sw(t, "main", SwitchOptions{Shelve: true})
	hash := res.Shelved.Hash
	theirs := ""
	race := hookRunner{before: func(dir string, args []string) {
		if isStash(args, "drop") {
			f.write(t, "other.txt", "theirs\n")
			f.r.Git(f.r.Dir, "stash", "push", "-q", "-u", "-m", "theirs", "--", "other.txt")
			theirs = f.r.Git(f.r.Dir, "rev-parse", "refs/stash")
		}
	}}
	back := f.swWith(t, race, "feature/login", SwitchOptions{Shelve: true})
	if !back.OK || back.Restored == nil || !strings.Contains(back.Restored.Why, theirs) || !strings.Contains(back.Restored.Why, "git stash store") {
		t.Fatalf("back = %+v (theirs %s)", back, theirs)
	}
	if got := f.stashList(); !slices.Equal(got, []string{hash}) {
		t.Fatalf("stash list = %v, want ours still there", got)
	}
	ops := f.opsOf(t)
	if last := ops[len(ops)-1]; last.Op != "drop-shelf" || !strings.HasPrefix(last.Result, "verify-failed: ") {
		t.Fatalf("drop entry = %+v", last)
	}
}

// git stash apply reports success but the folder does not hold the shelf:
// nothing is dropped.
func TestRestoreVerifiesBeforeDrop(t *testing.T) {
	f := newFixture(t)
	f.onLogin(t)
	f.write(t, "README", "edited\n")
	res := f.sw(t, "main", SwitchOptions{Shelve: true})
	hollow := skipRunner{skip: func(args []string) bool { return isStash(args, "apply") }}
	back := f.swWith(t, hollow, "feature/login", SwitchOptions{Shelve: true})
	if !back.OK || back.Restored != nil || back.KeptShelf == nil || back.KeptShelf.Why == "" {
		t.Fatalf("back = %+v", back)
	}
	if got := f.stashList(); !slices.Equal(got, []string{res.Shelved.Hash}) {
		t.Fatalf("stash list = %v", got)
	}
	ops := f.opsOf(t)
	if last := ops[len(ops)-1]; last.Op != "unshelve" || !strings.HasPrefix(last.Result, "verify-failed: ") {
		t.Fatalf("unshelve entry = %+v", last)
	}
}

// git stash push fails: nothing is switched and the folder keeps its work.
func TestShelfFailedSwitchesNothing(t *testing.T) {
	f := newFixture(t)
	f.onLogin(t)
	f.write(t, "README", "edited\n")
	run := failRunner{fail: func(args []string) bool { return isStash(args, "push") }}
	res := f.swWith(t, run, "main", SwitchOptions{Shelve: true})
	if res.OK || res.Reason != "shelf-failed" || res.Detail == "" || res.Shelved != nil || res.KeptShelf != nil {
		t.Fatalf("res = %+v", res)
	}
	if f.head(f.r.Dir) != "refs/heads/feature/login" || f.read(t, "README") != "edited\n" || len(f.stashList()) != 0 {
		t.Fatal("something changed")
	}
	ops := wantOps(t, f, "shelve feature/login "+f.opsOf(t)[0].Result)
	if !strings.HasPrefix(ops[0].Result, "git-failed: ") {
		t.Fatalf("shelve entry = %+v", ops[0])
	}
}

// Changes inside a submodule cannot be shelved by git stash: refused before
// anything runs.
func TestShelfRefusesSubmoduleChanges(t *testing.T) {
	f := newFixture(t)
	sub := submoduleFixture(t, f)
	f.r.Git(f.r.Dir, "commit", "-q", "-m", "add sub")
	if err := os.WriteFile(filepath.Join(sub, "README"), []byte("inside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := f.sw(t, "x", SwitchOptions{Shelve: true})
	if res.OK || res.Reason != "shelf-failed" || !slices.Equal(res.Files, []string{"sub"}) || res.Detail == "" {
		t.Fatalf("res = %+v", res)
	}
	if f.head(f.r.Dir) != "refs/heads/main" || len(f.stashList()) != 0 || len(f.opsOf(t)) != 0 {
		t.Fatal("something ran")
	}
}

// A folder stopped in a bisect, merge or rebase is not switched, and nothing
// is shelved.
func TestSwitchRefusesInProgress(t *testing.T) {
	f := newFixture(t)
	f.r.Git(f.r.Dir, "bisect", "start")
	f.write(t, "README", "edited\n")
	res := f.sw(t, "x", SwitchOptions{Shelve: true})
	if res.OK || res.Reason != "in-progress" {
		t.Fatalf("res = %+v", res)
	}
	if f.head(f.r.Dir) != "refs/heads/main" || f.read(t, "README") != "edited\n" || len(f.stashList()) != 0 {
		t.Fatal("something changed")
	}
}

// Without shelve, a dirty folder is refused as before and nothing is stashed.
func TestSwitchDirtyWithoutShelveRefuses(t *testing.T) {
	f := newFixture(t)
	f.onLogin(t)
	f.write(t, "new.txt", "new\n")
	res := f.sw(t, "main", SwitchOptions{Latest: true})
	if res.OK || res.Reason != "dirty" || !slices.Equal(res.Files, []string{"new.txt"}) || res.Shelved != nil {
		t.Fatalf("res = %+v", res)
	}
	if f.head(f.r.Dir) != "refs/heads/feature/login" || len(f.stashList()) != 0 || len(f.opsOf(t)) != 0 {
		t.Fatal("something changed")
	}
}
