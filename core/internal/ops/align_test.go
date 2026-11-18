package ops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/repo"
	"github.com/banma05/git-glance/core/internal/settings"
	"github.com/banma05/git-glance/core/internal/testrepo"
)

type fixture struct {
	r     *testrepo.Repo
	other string
	st    settings.Store
	run   gitx.ExecRunner
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	t.Setenv("GITGLANCE_HOME", t.TempDir())
	r := testrepo.New(t)
	r.ApplyEnv()
	f := &fixture{r: r, st: settings.Store{Dir: t.TempDir()}, run: gitx.ExecRunner{}}
	for _, b := range []string{"x", "y", "z", "w", "v", "u"} {
		r.Branch(b, "main")
		r.Push(r.Dir, "-u", "origin", b)
	}
	f.other = r.Clone("other")
	return f
}

// pushOther adds one commit to branch b of the remote from the second clone.
func (f *fixture) pushOther(b, file string) {
	f.r.Git(f.other, "checkout", b)
	f.r.Commit(f.other, "other "+b, map[string]string{file: b + "\n"})
	f.r.Push(f.other, "origin", b)
}

func (f *fixture) localCommit(b, file string) {
	f.r.Git(f.r.Dir, "checkout", b)
	f.r.Commit(f.r.Dir, "local "+b, map[string]string{file: b + "\n"})
	f.r.Git(f.r.Dir, "checkout", "main")
}

func (f *fixture) rev(ref string) string {
	return f.r.Git(f.r.Dir, "rev-parse", ref)
}

func hasSkip(rep AlignReport, branch, reason string) (AlignSkip, bool) {
	for _, s := range rep.Skipped {
		if s.Branch == branch && s.Reason == reason {
			return s, true
		}
	}
	return AlignSkip{}, false
}

func updatedNames(rep AlignReport) []string {
	names := []string{}
	for _, u := range rep.Updated {
		names = append(names, u.Branch)
	}
	return names
}

func TestAlign(t *testing.T) {
	f := newFixture(t)
	r := f.r
	yWT := r.AddWorktree("y", "y")
	zWT := r.AddWorktree("z", "z")
	for _, b := range []string{"x", "y", "z", "w"} {
		f.pushOther(b, b+".txt")
	}
	f.localCommit("w", "w-local.txt")
	f.localCommit("v", "v-local.txt")
	r.Git(f.other, "push", "origin", "--delete", "u")
	if err := os.WriteFile(filepath.Join(zWT, "README"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	vBefore, wBefore, zBefore := f.rev("v"), f.rev("w"), f.rev("z")

	rep, err := Align(context.Background(), f.run, f.st, r.Dir, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Fetch.OK {
		t.Fatalf("fetch: %+v", rep.Fetch)
	}
	if got := strings.Join(updatedNames(rep), ","); got != "x,y" {
		t.Fatalf("updated = %q, report %+v", got, rep)
	}
	for _, b := range []string{"x", "y"} {
		if f.rev(b) != f.rev("origin/"+b) {
			t.Fatalf("%s not aligned", b)
		}
	}
	if _, err := os.Stat(filepath.Join(yWT, "y.txt")); err != nil {
		t.Fatalf("y folder lacks the new file: %v", err)
	}
	for _, u := range rep.Updated {
		if u.From == "" || u.To == "" || u.From == u.To {
			t.Fatalf("bad item %+v", u)
		}
		if u.Branch == "y" && !strings.HasSuffix(u.Folder, "wt-y") {
			t.Fatalf("folder = %q", u.Folder)
		}
		if u.Branch == "x" && u.Folder != "" {
			t.Fatalf("x has folder %q", u.Folder)
		}
	}
	z, ok := hasSkip(rep, "z", "dirty")
	if !ok || len(z.Files) == 0 || z.Files[0] != "README" {
		t.Fatalf("z skip = %+v ok=%v", z, ok)
	}
	if _, ok := hasSkip(rep, "w", "diverged"); !ok {
		t.Fatalf("no w diverged: %+v", rep.Skipped)
	}
	if _, ok := hasSkip(rep, "u", "upstream-gone"); !ok {
		t.Fatalf("no u upstream-gone: %+v", rep.Skipped)
	}
	for _, s := range rep.Skipped {
		if s.Branch == "v" {
			t.Fatalf("v must not be listed: %+v", s)
		}
	}
	if f.rev("v") != vBefore || f.rev("w") != wBefore || f.rev("z") != zBefore {
		t.Fatal("a skipped branch moved")
	}
	ops, err := f.st.Ops("", 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, o := range ops {
		if o.Op == "align" {
			n++
			if o.Before == "" || o.After == "" || o.Repo != r.Dir || o.Result != "ok" {
				t.Fatalf("bad op %+v", o)
			}
			want, folder := "", ""
			switch o.Branch {
			case "x":
				want = "git fetch . refs/remotes/origin/x:refs/heads/x"
			case "y":
				want = "git merge --ff-only --no-overwrite-ignore --no-autostash refs/remotes/origin/y"
				folder = yWT
			}
			if strings.Join(o.Command, " ") != want || o.Folder != folder {
				t.Fatalf("op %s: command %q folder %q", o.Branch, o.Command, o.Folder)
			}
		}
	}
	if n != 2 {
		t.Fatalf("align ops = %d, want 2", n)
	}
}

func TestAlignAheadSkipped(t *testing.T) {
	f := newFixture(t)
	f.localCommit("v", "v-local.txt")
	rep, err := Align(context.Background(), f.run, f.st, f.r.Dir, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	// "v" is only ahead: nothing to pull, so it is not listed anywhere.
	if len(rep.Updated) != 0 || len(rep.Skipped) != 0 {
		t.Fatalf("report: %+v", rep)
	}
	if rep.Updated == nil || rep.Skipped == nil {
		t.Fatal("slices must be non-nil")
	}
}

func TestAlignFetchFails(t *testing.T) {
	f := newFixture(t)
	f.pushOther("x", "x.txt")
	before := map[string]string{}
	for _, b := range []string{"x", "y", "main"} {
		before[b] = f.rev(b)
	}
	if err := os.Rename(f.r.Remote, f.r.Remote+"-moved"); err != nil {
		t.Fatal(err)
	}
	rep, err := Align(context.Background(), f.run, f.st, f.r.Dir, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Fetch.OK || rep.Fetch.Attempts != 2 || rep.Fetch.Error == "" || rep.Fetch.At == 0 {
		t.Fatalf("fetch = %+v", rep.Fetch)
	}
	if rep.Updated == nil || rep.Skipped == nil || len(rep.Updated) != 0 || len(rep.Skipped) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	for b, h := range before {
		if f.rev(b) != h {
			t.Fatalf("%s moved", b)
		}
	}
}

func TestAlignIndexLock(t *testing.T) {
	f := newFixture(t)
	yWT := f.r.AddWorktree("y", "y")
	f.pushOther("x", "x.txt")
	f.pushOther("y", "y.txt")
	yBefore := f.rev("y")
	lock := filepath.Join(f.r.Dir, ".git", "worktrees", filepath.Base(yWT), "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := Align(context.Background(), f.run, f.st, f.r.Dir, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := hasSkip(rep, "y", "git-failed")
	if !ok || s.Detail == "" {
		t.Fatalf("y skip = %+v ok=%v, report %+v", s, ok, rep)
	}
	if got := strings.Join(updatedNames(rep), ","); got != "x" {
		t.Fatalf("updated = %q", got)
	}
	if f.rev("y") != yBefore {
		t.Fatal("y moved")
	}
}

func TestAlignFolderMissing(t *testing.T) {
	f := newFixture(t)
	yWT := f.r.AddWorktree("y", "y")
	f.pushOther("y", "y.txt")
	yBefore := f.rev("y")
	if err := os.RemoveAll(yWT); err != nil {
		t.Fatal(err)
	}
	rep, err := Align(context.Background(), f.run, f.st, f.r.Dir, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := hasSkip(rep, "y", "folder-missing"); !ok {
		t.Fatalf("no folder-missing skip: %+v", rep)
	}
	if f.rev("y") != yBefore {
		t.Fatal("y moved")
	}
}

func TestAlignIgnoresLocalUpstream(t *testing.T) {
	f := newFixture(t)
	f.r.Git(f.r.Dir, "branch", "--track", "t1", "main")
	f.r.Commit(f.r.Dir, "main moves", map[string]string{"m.txt": "m\n"})
	rep, err := Align(context.Background(), f.run, f.st, f.r.Dir, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range rep.Skipped {
		if s.Branch == "t1" {
			t.Fatalf("t1 listed: %+v", s)
		}
	}
	for _, u := range rep.Updated {
		if u.Branch == "t1" {
			t.Fatalf("t1 updated: %+v", u)
		}
	}
}

func (f *fixture) staleSnapshot(t *testing.T) repo.Snapshot {
	t.Helper()
	f.r.Git(f.r.Dir, "fetch", "--all", "--prune")
	snap, err := repo.Read(context.Background(), f.run, f.r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func (f *fixture) alignStale(t *testing.T, snap repo.Snapshot) AlignReport {
	t.Helper()
	rep, err := alignSnapshot(context.Background(), f.run, f.st, f.r.Dir, snap, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestAlignFolderSwitchedAfterSnapshot(t *testing.T) {
	f := newFixture(t)
	yWT := f.r.AddWorktree("y", "y")
	f.r.Branch("release", "main")
	f.pushOther("y", "y.txt")
	snap := f.staleSnapshot(t)
	f.r.Git(yWT, "switch", "release")
	releaseBefore := f.rev("release")
	rep := f.alignStale(t, snap)
	if f.rev("release") != releaseBefore {
		t.Fatal("release moved")
	}
	s, ok := hasSkip(rep, "y", "git-failed")
	if !ok || !strings.Contains(s.Detail, "HEAD") {
		t.Fatalf("report %+v", rep)
	}
	if len(rep.Updated) != 0 {
		t.Fatalf("updated %+v", rep.Updated)
	}
}

func TestAlignBranchMovedAfterSnapshot(t *testing.T) {
	f := newFixture(t)
	yWT := f.r.AddWorktree("y", "y")
	f.pushOther("y", "y.txt")
	snap := f.staleSnapshot(t)
	f.r.Git(yWT, "merge", "origin/y")
	f.r.Commit(yWT, "own work", map[string]string{"own.txt": "own\n"})
	own := f.rev("y")
	rep := f.alignStale(t, snap)
	if _, ok := hasSkip(rep, "y", "git-failed"); !ok || len(rep.Updated) != 0 {
		t.Fatalf("report %+v", rep)
	}
	if f.rev("y") != own {
		t.Fatal("y moved")
	}
	ops, _ := f.st.Ops("", 0)
	if len(ops) != 0 {
		t.Fatalf("ops logged: %+v", ops)
	}
}

func TestAlignCheckedOutTwice(t *testing.T) {
	f := newFixture(t)
	f.r.AddWorktree("y1", "y")
	wt2 := f.r.Root + "/wt-y2"
	f.r.Git(f.r.Dir, "worktree", "add", "-f", wt2, "y")
	f.pushOther("y", "y.txt")
	before := f.rev("y")
	rep, err := Align(context.Background(), f.run, f.st, f.r.Dir, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := hasSkip(rep, "y", "checked-out-twice"); !ok || f.rev("y") != before {
		t.Fatalf("report %+v", rep)
	}
	// A dirty second holder is reported as dirty, not as checked-out-twice.
	if err := os.WriteFile(filepath.Join(wt2, "README"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err = Align(context.Background(), f.run, f.st, f.r.Dir, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := hasSkip(rep, "y", "dirty"); !ok || f.rev("y") != before {
		t.Fatalf("report %+v", rep)
	}
}

func TestAlignKeepsIgnoredFile(t *testing.T) {
	f := newFixture(t)
	yWT := f.r.AddWorktree("y", "y")
	f.pushOther("y", "secret.env")
	if err := os.WriteFile(f.r.Dir+"/.git/info/exclude", []byte("secret.env\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(yWT, "secret.env")
	if err := os.WriteFile(secret, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := f.rev("y")
	rep, err := Align(context.Background(), f.run, f.st, f.r.Dir, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := hasSkip(rep, "y", "git-failed")
	if !ok || s.Detail == "" {
		t.Fatalf("report %+v", rep)
	}
	got, _ := os.ReadFile(secret)
	if string(got) != "mine\n" || f.rev("y") != before {
		t.Fatalf("ignored file lost or branch moved: %q", got)
	}
}

func TestFetchKeepsLocalTags(t *testing.T) {
	f := newFixture(t)
	f.r.Git(f.r.Dir, "config", "fetch.pruneTags", "true")
	f.r.Git(f.r.Dir, "tag", "local-only")
	res := Fetch(context.Background(), f.run, f.r.Dir, time.Now)
	if !res.OK {
		t.Fatalf("fetch %+v", res)
	}
	if f.r.Git(f.r.Dir, "tag", "--list", "local-only") != "local-only" {
		t.Fatal("local tag was pruned")
	}
}

func TestAlignOpLogFailure(t *testing.T) {
	f := newFixture(t)
	f.pushOther("x", "x.txt")
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := settings.Store{Dir: blocker}
	rep, err := Align(context.Background(), f.run, st, f.r.Dir, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Updated) != 1 || rep.Updated[0].Branch != "x" || rep.LogError == "" {
		t.Fatalf("report %+v", rep)
	}
}

func TestAlignSameNamedTag(t *testing.T) {
	f := newFixture(t)
	f.r.Branch("v1", "main")
	f.r.Push(f.r.Dir, "-u", "origin", "v1")
	wt := f.r.AddWorktree("v1", "v1")
	f.r.Git(f.r.Dir, "tag", "v1", "main")
	f.r.Git(f.other, "fetch", "origin")
	f.pushOther("v1", "v1.txt")
	rep, err := Align(context.Background(), f.run, f.st, f.r.Dir, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if f.r.Git(f.r.Dir, "rev-parse", "refs/heads/v1") != f.r.Git(f.r.Dir, "rev-parse", "refs/remotes/origin/v1") {
		t.Fatalf("v1 not aligned: %+v", rep)
	}
	found := false
	for _, u := range rep.Updated {
		found = found || (u.Branch == "v1" && u.Folder == wt)
	}
	if !found {
		t.Fatalf("v1 not in Updated: %+v", rep)
	}
}

func TestAlignSkipsHiddenChanges(t *testing.T) {
	f := newFixture(t)
	wt := f.r.AddWorktree("y", "y")
	f.pushOther("y", "y.txt")
	f.r.Git(wt, "update-index", "--assume-unchanged", "README")
	if err := os.WriteFile(filepath.Join(wt, "README"), []byte("edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := Align(context.Background(), f.run, f.st, f.r.Dir, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := hasSkip(rep, "y", "hidden-changes")
	if !ok || len(s.Files) != 1 || s.Files[0] != "README" {
		t.Fatalf("report = %+v", rep)
	}
}

func TestAlignSparseEntryIsNotHidden(t *testing.T) {
	f := newFixture(t)
	wt := f.r.AddWorktree("y", "y")
	f.pushOther("y", "y.txt")
	f.r.Git(wt, "update-index", "--skip-worktree", "README")
	if err := os.Remove(filepath.Join(wt, "README")); err != nil {
		t.Fatal(err)
	}
	rep, err := Align(context.Background(), f.run, f.st, f.r.Dir, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if got := updatedNames(rep); len(got) != 1 || got[0] != "y" {
		t.Fatalf("report = %+v", rep)
	}
}

// A feature branch set to follow a trunk (origin/main) must never be moved by
// Align: that would carry the trunk into it.
func TestAlignOnlyBranchesTrackingThemselves(t *testing.T) {
	f := newFixture(t)
	f.r.Git(f.r.Dir, "branch", "--track", "alice/x", "origin/main")
	f.pushOther("main", "m.txt")
	before := f.rev("alice/x")
	rep, err := Align(context.Background(), f.run, f.st, f.r.Dir, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := hasSkip(rep, "alice/x", "tracks-other-branch"); !ok || f.rev("alice/x") != before {
		t.Fatalf("alice/x moved or not reported: %+v", rep)
	}
	if got := strings.Join(updatedNames(rep), ","); got != "main" || f.rev("main") != f.rev("origin/main") {
		t.Fatalf("updated = %q, want main aligned: %+v", got, rep)
	}
}

// A branch that a stopped rebase or a bisect works on belongs to that
// operation: moving it would be overwritten or confuse it.
func TestAlignSkipsBranchesHeldByRebaseOrBisect(t *testing.T) {
	f := newFixture(t)
	f.r.Commit(f.r.Dir, "second", map[string]string{"s.txt": "s\n"})
	f.r.Push(f.r.Dir, "origin", "main")
	for _, b := range []string{"rb", "bs"} {
		f.r.Branch(b, "main")
		f.r.Push(f.r.Dir, "-u", "origin", b)
	}
	f.r.Git(f.other, "fetch", "origin")
	f.pushOther("rb", "rb.txt")
	f.pushOther("bs", "bs.txt")
	rbWT := f.r.AddWorktree("rb", "rb")
	cmd := exec.Command("git", "rebase", "-f", "-x", "false", "HEAD~1")
	cmd.Dir, cmd.Env = rbWT, f.r.Env()
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("the rebase did not stop: %s", out)
	}
	bsWT := f.r.AddWorktree("bs", "bs")
	f.r.Git(bsWT, "bisect", "start")
	before := map[string]string{"rb": f.rev("rb"), "bs": f.rev("bs")}
	rep, err := Align(context.Background(), f.run, f.st, f.r.Dir, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	for b, h := range before {
		if _, ok := hasSkip(rep, b, "in-progress"); !ok || f.rev(b) != h {
			t.Fatalf("%s moved or not reported as in-progress: %+v", b, rep)
		}
	}
}

// One remote that cannot be reached must not keep the others from aligning.
func TestAlignAfterOneRemoteFailed(t *testing.T) {
	f := newFixture(t)
	f.r.Git(f.r.Dir, "remote", "add", "broken", f.r.Root+"/no-such.git")
	f.pushOther("x", "x.txt")
	rep, err := Align(context.Background(), f.run, f.st, f.r.Dir, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Fetch.OK || rep.Fetch.Error == "" {
		t.Fatalf("fetch = %+v, want the failure reported", rep.Fetch)
	}
	if got := strings.Join(updatedNames(rep), ","); got != "x" || f.rev("x") != f.rev("origin/x") {
		t.Fatalf("updated = %q, want x: %+v", got, rep)
	}
}
