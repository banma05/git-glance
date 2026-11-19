package ops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/banma05/git-glance/core/internal/settings"
)

func fixedNow() time.Time { return time.Unix(1700000000, 0) }

func (f *fixture) head(dir string) string {
	return f.r.Git(dir, "rev-parse", "--symbolic-full-name", "HEAD")
}

func (f *fixture) opNames(t *testing.T) []string {
	t.Helper()
	ops, err := f.st.Ops(f.r.Dir, 100)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, o := range ops {
		names = append(names, o.Op)
	}
	return names
}

func TestSwitchDirty(t *testing.T) {
	f := newFixture(t)
	f.r.Branch("feature/b", "main")
	before := f.rev("HEAD")
	if err := os.WriteFile(filepath.Join(f.r.Dir, "README"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Switch(context.Background(), f.run, f.st, f.r.Dir, f.r.Dir, "feature/b", SwitchOptions{}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Reason != "dirty" || len(res.Files) != 1 || res.Files[0] != "README" {
		t.Fatalf("res = %+v, want dirty with README", res)
	}
	if f.rev("HEAD") != before || f.head(f.r.Dir) != "refs/heads/main" {
		t.Fatal("HEAD moved")
	}
	if len(f.opNames(t)) != 0 {
		t.Fatal("a refused switch must not be logged")
	}
}

func TestSwitchCheckedOutElsewhere(t *testing.T) {
	f := newFixture(t)
	f.r.Branch("feature/a", "main")
	wtA := f.r.AddWorktree("a", "feature/a")
	res, err := Switch(context.Background(), f.run, f.st, f.r.Dir, f.r.Dir, "feature/a", SwitchOptions{}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Reason != "checked-out-elsewhere" || res.Path != wtA {
		t.Fatalf("res = %+v, want checked-out-elsewhere at %s", res, wtA)
	}
	if f.head(f.r.Dir) != "refs/heads/main" {
		t.Fatal("HEAD moved")
	}
}

func TestSwitchOK(t *testing.T) {
	f := newFixture(t)
	f.r.Branch("feature/b", "main")
	res, err := Switch(context.Background(), f.run, f.st, f.r.Dir, f.r.Dir, "feature/b", SwitchOptions{}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Files == nil || res.LogError != "" {
		t.Fatalf("res = %+v, want ok", res)
	}
	if got := f.head(f.r.Dir); got != "refs/heads/feature/b" {
		t.Fatalf("HEAD = %q", got)
	}
	if names := f.opNames(t); len(names) != 1 || names[0] != "switch" {
		t.Fatalf("ops = %v, want [switch]", names)
	}
}

func TestSwitchRefusals(t *testing.T) {
	f := newFixture(t)
	f.r.Git(f.r.Dir, "fetch", "origin")
	f.r.Git(f.other, "push", "origin", "main:remote-only")
	f.r.Git(f.r.Dir, "fetch", "origin")
	f.r.Branch("feature/b", "main")
	wt := f.r.AddWorktree("b", "feature/b")
	f.r.Branch("feature/c", "main")

	res, err := Switch(context.Background(), f.run, f.st, f.r.Dir, f.r.Dir, "remote-only", SwitchOptions{}, fixedNow)
	if err != nil || res.OK || res.Reason != "no-branch" {
		t.Fatalf("remote-only: res = %+v, err = %v", res, err)
	}
	if got := f.r.Git(f.r.Dir, "for-each-ref", "refs/heads/remote-only"); got != "" {
		t.Fatal("a local branch was created from the remote one")
	}

	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	res, err = Switch(context.Background(), f.run, f.st, f.r.Dir, wt, "feature/c", SwitchOptions{}, fixedNow)
	if err != nil || res.OK || res.Reason != "folder-missing" {
		t.Fatalf("missing folder: res = %+v, err = %v", res, err)
	}

	if _, err = Switch(context.Background(), f.run, f.st, f.r.Dir, f.r.Root+"/nowhere", "feature/c", SwitchOptions{}, fixedNow); err == nil {
		t.Fatal("an unknown folder must be an error")
	}
}

func TestSwitchIgnoredFileIsNotOverwritten(t *testing.T) {
	f := newFixture(t)
	f.r.Git(f.r.Dir, "switch", "-c", "feature/b")
	f.r.Commit(f.r.Dir, "tracked file", map[string]string{"gen.txt": "tracked\n"})
	f.r.Git(f.r.Dir, "switch", "main")
	exclude := filepath.Join(f.r.Dir, ".git", "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(exclude), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exclude, []byte("gen.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.r.Dir, "gen.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Switch(context.Background(), f.run, f.st, f.r.Dir, f.r.Dir, "feature/b", SwitchOptions{}, fixedNow)
	if err != nil || res.OK || res.Reason != "git-failed" || res.Detail == "" || res.Files == nil {
		t.Fatalf("res = %+v, err = %v, want git-failed over an ignored file", res, err)
	}
	if got := f.head(f.r.Dir); got != "refs/heads/main" {
		t.Fatalf("HEAD = %q", got)
	}
	data, rerr := os.ReadFile(filepath.Join(f.r.Dir, "gen.txt"))
	if rerr != nil || strings.TrimSpace(string(data)) != "mine" {
		t.Fatalf("ignored file was lost: %q %v", data, rerr)
	}
	// git ran and failed: the attempt is in the log all the same.
	if e, ok := opFor(t, f, "switch", "feature/b"); !ok || !strings.HasPrefix(e.Result, "git-failed: ") || e.After != "refs/heads/main" {
		t.Fatalf("log entry = %+v (found %v)", e, ok)
	}
}

func TestSwitchVerifyFailedIsLogged(t *testing.T) {
	f := newFixture(t)
	f.r.Branch("feature/b", "main")
	run := skipRunner{skip: func(args []string) bool { return args[0] == "switch" }}
	res, err := Switch(context.Background(), run, f.st, f.r.Dir, f.r.Dir, "feature/b", SwitchOptions{}, fixedNow)
	if err != nil || res.OK || res.Reason != "verify-failed" || !strings.Contains(res.Detail, "refs/heads/main") {
		t.Fatalf("res = %+v, err = %v, want verify-failed", res, err)
	}
	if e, ok := opFor(t, f, "switch", "feature/b"); !ok || !strings.HasPrefix(e.Result, "verify-failed: ") {
		t.Fatalf("log entry = %+v (found %v)", e, ok)
	}
}

func TestSwitchLogErrorKeepsOK(t *testing.T) {
	f := newFixture(t)
	f.r.Branch("feature/b", "main")
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := settings.Store{Dir: filepath.Join(blocker, "sub")}
	res, err := Switch(context.Background(), f.run, st, f.r.Dir, f.r.Dir, "feature/b", SwitchOptions{}, fixedNow)
	if err != nil || !res.OK || res.LogError == "" {
		t.Fatalf("res = %+v, err = %v, want ok with a log error", res, err)
	}
}

func TestSwitchDetached(t *testing.T) {
	f := newFixture(t)
	f.r.Branch("feature/d", "main")
	f.r.Branch("feature/e", "main")
	wt := f.r.AddWorktree("d", "feature/d")
	f.r.Git(wt, "switch", "--detach")
	at := f.r.Git(wt, "rev-parse", "HEAD")
	res, err := Switch(context.Background(), f.run, f.st, f.r.Dir, wt, "feature/e", SwitchOptions{}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Reason != "detached" || res.Files == nil {
		t.Fatalf("res = %+v, want detached", res)
	}
	if f.head(wt) != "HEAD" || f.r.Git(wt, "rev-parse", "HEAD") != at {
		t.Fatal("the detached folder moved")
	}
	if len(f.opNames(t)) != 0 {
		t.Fatal("a refused switch must not be logged")
	}
}

func TestSwitchRefusesHiddenChanges(t *testing.T) {
	f := newFixture(t)
	wt := f.r.AddWorktree("y", "y")
	f.r.Git(wt, "update-index", "--assume-unchanged", "README")
	if err := os.WriteFile(filepath.Join(wt, "README"), []byte("edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Switch(context.Background(), f.run, f.st, f.r.Dir, wt, "x", SwitchOptions{}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Reason != "hidden-changes" || len(res.Files) != 1 || res.Files[0] != "README" {
		t.Fatalf("res = %+v", res)
	}
	if f.head(wt) != "refs/heads/y" {
		t.Fatal("HEAD moved")
	}
}

func TestSwitchSparseEntryIsNotHidden(t *testing.T) {
	f := newFixture(t)
	wt := f.r.AddWorktree("y", "y")
	f.r.Git(wt, "update-index", "--skip-worktree", "README")
	if err := os.Remove(filepath.Join(wt, "README")); err != nil {
		t.Fatal(err)
	}
	res, err := Switch(context.Background(), f.run, f.st, f.r.Dir, wt, "x", SwitchOptions{}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("res = %+v, want ok", res)
	}
}

// mainBehind puts the main folder on x and adds one commit to origin/main
// that local main lacks.
func mainBehind(t *testing.T, f *fixture) {
	t.Helper()
	f.r.Git(f.r.Dir, "switch", "-q", "x")
	f.pushOther("main", "m.txt")
	f.r.Git(f.r.Dir, "fetch", "-q", "origin")
}

// mainAhead adds one commit to local main that origin/main lacks; main must
// not be checked out.
func mainAhead(t *testing.T, f *fixture) {
	t.Helper()
	wt := f.r.AddWorktree("m", "main")
	f.r.Commit(wt, "local main", map[string]string{"local.txt": "local\n"})
	f.r.Git(f.r.Dir, "worktree", "remove", wt)
}

func TestSwitchLatestFastForwards(t *testing.T) {
	f := newFixture(t)
	mainBehind(t, f)
	res := f.sw(t, "main", SwitchOptions{Latest: true})
	if !res.OK || !res.FastForwarded || res.AheadNote != "" || res.LogError != "" {
		t.Fatalf("res = %+v", res)
	}
	if f.rev("main") != f.rev("origin/main") || f.head(f.r.Dir) != "refs/heads/main" || f.read(t, "m.txt") != "main\n" {
		t.Fatal("main is not at origin/main in the folder")
	}
	ops := wantOps(t, f, "align main ok", "switch main ok")
	if strings.Join(ops[0].Command, " ") != "git fetch . refs/remotes/origin/main:refs/heads/main" {
		t.Fatalf("align entry = %+v", ops[0])
	}
}

// The folder is already on the branch: it is fast-forwarded in place.
func TestSwitchLatestInPlace(t *testing.T) {
	f := newFixture(t)
	f.pushOther("main", "m.txt")
	f.r.Git(f.r.Dir, "fetch", "-q", "origin")
	res := f.sw(t, "main", SwitchOptions{Latest: true, Shelve: true})
	if !res.OK || !res.FastForwarded || res.Shelved != nil {
		t.Fatalf("res = %+v", res)
	}
	if f.rev("main") != f.rev("origin/main") || f.read(t, "m.txt") != "main\n" {
		t.Fatal("main was not fast-forwarded")
	}
	ops := wantOps(t, f, "align main ok")
	if strings.Join(ops[0].Command, " ") != "git merge --ff-only --no-overwrite-ignore --no-autostash refs/remotes/origin/main" {
		t.Fatalf("align entry = %+v", ops[0])
	}
	if st := f.state(t); st.Previous != "" {
		t.Fatalf("no switch happened, previous = %q", st.Previous)
	}
}

// Moving the folder's own branch would leave its work on a commit it was not
// made on: a dirty folder already on the branch is refused, shelve or not.
func TestSwitchLatestInPlaceRefusesDirty(t *testing.T) {
	f := newFixture(t)
	f.pushOther("main", "m.txt")
	f.r.Git(f.r.Dir, "fetch", "-q", "origin")
	before := f.rev("main")
	f.write(t, "README", "edited\n")
	res := f.sw(t, "main", SwitchOptions{Latest: true, Shelve: true})
	if res.OK || res.Reason != "dirty" || res.Shelved != nil {
		t.Fatalf("res = %+v", res)
	}
	if f.rev("main") != before || f.read(t, "README") != "edited\n" || len(f.stashList()) != 0 {
		t.Fatal("something changed")
	}
}

func TestSwitchLatestKeepsAheadBranch(t *testing.T) {
	f := newFixture(t)
	f.r.Git(f.r.Dir, "switch", "-q", "x")
	mainAhead(t, f)
	before := f.rev("main")
	res := f.sw(t, "main", SwitchOptions{Latest: true})
	if !res.OK || res.FastForwarded || res.AheadNote != "local has 1 commit not on origin/main; not fast-forwarded" {
		t.Fatalf("res = %+v", res)
	}
	if f.rev("main") != before || f.head(f.r.Dir) != "refs/heads/main" {
		t.Fatal("main moved or the folder is not on it")
	}
	wantOps(t, f, "switch main ok")

	// Ahead and behind: still not moved, the note says so.
	f.r.Git(f.r.Dir, "switch", "-q", "x")
	f.pushOther("main", "m.txt")
	f.r.Git(f.r.Dir, "fetch", "-q", "origin")
	res = f.sw(t, "main", SwitchOptions{Latest: true})
	if !res.OK || res.FastForwarded || res.AheadNote != "local has 1 commit not on origin/main; not fast-forwarded" || f.rev("main") != before {
		t.Fatalf("diverged: res = %+v", res)
	}
}

// A branch that follows a branch of another name (a trunk) is switched to
// but never moved to that branch.
func TestSwitchLatestOnlyOwnNameUpstream(t *testing.T) {
	f := newFixture(t)
	f.r.Git(f.r.Dir, "branch", "-q", "--track", "feature/login", "origin/main")
	f.pushOther("main", "m.txt")
	f.r.Git(f.r.Dir, "fetch", "-q", "origin")
	before := f.rev("feature/login")
	res := f.sw(t, "feature/login", SwitchOptions{Latest: true})
	if !res.OK || res.FastForwarded || res.AheadNote != "" || f.rev("feature/login") != before {
		t.Fatalf("res = %+v", res)
	}
	wantOps(t, f, "switch feature/login ok")
}

// The fast-forward fails after the shelf was taken: the work is put back.
func TestSwitchLatestFailureRestoresShelf(t *testing.T) {
	f := newFixture(t)
	mainBehind(t, f)
	f.write(t, "README", "edited\n")
	run := failRunner{fail: func(args []string) bool { return args[0] == "fetch" && args[1] == "." }}
	res := f.swWith(t, run, "main", SwitchOptions{Latest: true, Shelve: true})
	if res.OK || res.Reason != "git-failed" || res.Restored == nil || f.head(f.r.Dir) != "refs/heads/x" || f.read(t, "README") != "edited\n" {
		t.Fatalf("res = %+v", res)
	}
	if len(f.stashList()) != 0 || f.rev("main") == f.rev("origin/main") {
		t.Fatal("stash or main changed")
	}
}

func TestSwitchCreatesFromRemote(t *testing.T) {
	f := newFixture(t)
	f.r.Git(f.other, "switch", "-q", "-c", "v2")
	f.r.Commit(f.other, "v2 work", map[string]string{"v2.txt": "v2\n"})
	f.r.Push(f.other, "origin", "v2")
	f.r.Git(f.r.Dir, "fetch", "-q", "origin")
	f.write(t, "README", "edited\n")

	res := f.sw(t, "v2", SwitchOptions{Remote: "origin", Latest: true, Shelve: true})
	if !res.OK || res.Shelved == nil || res.FastForwarded || res.AheadNote != "" {
		t.Fatalf("res = %+v", res)
	}
	if f.head(f.r.Dir) != "refs/heads/v2" || f.rev("v2") != f.rev("origin/v2") || f.read(t, "v2.txt") != "v2\n" {
		t.Fatal("the folder is not on a local v2 at origin/v2")
	}
	if up := f.r.Git(f.r.Dir, "rev-parse", "--symbolic-full-name", "v2@{upstream}"); up != "refs/remotes/origin/v2" {
		t.Fatalf("upstream = %q", up)
	}
	ops := wantOps(t, f, "shelve main ok", "switch v2 ok")
	if strings.Join(ops[1].Command, " ") != "git switch --no-guess --no-overwrite-ignore -c v2 --track refs/remotes/origin/v2" ||
		ops[1].Before != "refs/heads/main" || ops[1].After != "refs/heads/v2" {
		t.Fatalf("switch entry = %+v", ops[1])
	}
	if st := f.state(t); st.Previous != "main" {
		t.Fatalf("previous = %q", st.Previous)
	}
}

func TestSwitchFromRemoteRefusals(t *testing.T) {
	f := newFixture(t)
	f.r.Git(f.other, "push", "-q", "origin", "main:v3")
	f.r.Git(f.r.Dir, "fetch", "-q", "origin")
	for _, opt := range []SwitchOptions{{}, {Remote: "nope"}, {Remote: "origin/v3"}} {
		res := f.sw(t, "v3", opt)
		if res.OK || res.Reason != "no-branch" {
			t.Fatalf("%+v: res = %+v", opt, res)
		}
	}
	if got := f.r.Git(f.r.Dir, "for-each-ref", "refs/heads/v3"); got != "" {
		t.Fatal("a local branch was created")
	}
	// A local branch of that name exists: remote is ignored.
	res := f.sw(t, "x", SwitchOptions{Remote: "origin"})
	if !res.OK || f.head(f.r.Dir) != "refs/heads/x" {
		t.Fatalf("res = %+v", res)
	}
	ops := wantOps(t, f, "switch x ok")
	if strings.Join(ops[0].Command, " ") != "git switch --no-guess --no-overwrite-ignore x" {
		t.Fatalf("switch entry = %+v", ops[0])
	}
}

func TestSwitchRecordsPrevious(t *testing.T) {
	f := newFixture(t)
	if st := f.state(t); st.Previous != "" || len(st.Shelves) != 0 {
		t.Fatalf("fresh state = %+v", st)
	}
	f.sw(t, "x", SwitchOptions{})
	if st := f.state(t); st.Previous != "main" {
		t.Fatalf("previous = %q, want main", st.Previous)
	}
	f.sw(t, "main", SwitchOptions{})
	if st := f.state(t); st.Previous != "x" {
		t.Fatalf("previous = %q, want x", st.Previous)
	}
	// A refused switch records nothing.
	f.write(t, "README", "edited\n")
	if res := f.sw(t, "y", SwitchOptions{}); res.Reason != "dirty" {
		t.Fatalf("res = %+v", res)
	}
	if st := f.state(t); st.Previous != "x" {
		t.Fatalf("previous = %q, want x", st.Previous)
	}
	// The folder went back by hand: there is nothing to switch back to.
	f.r.Git(f.r.Dir, "checkout", "-q", "README")
	f.r.Git(f.r.Dir, "switch", "-q", "x")
	if st := f.state(t); st.Previous != "" {
		t.Fatalf("on the previous branch already: previous = %q", st.Previous)
	}
	// The previous branch is gone.
	f.r.Git(f.r.Dir, "switch", "-q", "main")
	f.r.Git(f.r.Dir, "branch", "-q", "-D", "x")
	if st := f.state(t); st.Previous != "" {
		t.Fatalf("deleted branch: previous = %q", st.Previous)
	}
	// Each folder has its own.
	wt := f.r.AddWorktree("y", "y")
	if _, err := Switch(context.Background(), f.run, f.st, f.r.Dir, wt, "z", SwitchOptions{}, fixedNow); err != nil {
		t.Fatal(err)
	}
	st, err := ReadSwitchState(context.Background(), f.run, f.st, f.r.Dir, wt)
	if err != nil || st.Previous != "y" {
		t.Fatalf("other folder: %+v, %v", st, err)
	}
	if mine := f.state(t); mine.Previous != "" {
		t.Fatalf("main folder previous = %q", mine.Previous)
	}
}
