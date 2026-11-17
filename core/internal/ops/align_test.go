package ops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/banma05/git-glance/core/internal/gitx"
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
			if o.Before == "" || o.After == "" || len(o.Command) == 0 || o.Repo != r.Dir {
				t.Fatalf("bad op %+v", o)
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
