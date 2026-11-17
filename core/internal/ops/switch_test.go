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
	res, err := Switch(context.Background(), f.run, f.st, f.r.Dir, f.r.Dir, "feature/b", fixedNow)
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
	res, err := Switch(context.Background(), f.run, f.st, f.r.Dir, f.r.Dir, "feature/a", fixedNow)
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
	res, err := Switch(context.Background(), f.run, f.st, f.r.Dir, f.r.Dir, "feature/b", fixedNow)
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

	res, err := Switch(context.Background(), f.run, f.st, f.r.Dir, f.r.Dir, "remote-only", fixedNow)
	if err != nil || res.OK || res.Reason != "no-branch" {
		t.Fatalf("remote-only: res = %+v, err = %v", res, err)
	}
	if got := f.r.Git(f.r.Dir, "for-each-ref", "refs/heads/remote-only"); got != "" {
		t.Fatal("a local branch was created from the remote one")
	}

	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	res, err = Switch(context.Background(), f.run, f.st, f.r.Dir, wt, "feature/c", fixedNow)
	if err != nil || res.OK || res.Reason != "folder-missing" {
		t.Fatalf("missing folder: res = %+v, err = %v", res, err)
	}

	if _, err = Switch(context.Background(), f.run, f.st, f.r.Dir, f.r.Root+"/nowhere", "feature/c", fixedNow); err == nil {
		t.Fatal("an unknown folder must be an error")
	}
}

func TestSwitchIgnoredFileIsNotOverwritten(t *testing.T) {
	f := newFixture(t)
	f.r.Git(f.r.Dir, "switch", "-c", "feature/b")
	f.r.Commit(f.r.Dir, "tracked file", map[string]string{"gen.txt": "tracked\n", ".gitignore": "gen.txt\n"})
	f.r.Git(f.r.Dir, "switch", "main")
	if err := os.WriteFile(filepath.Join(f.r.Dir, "gen.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ := Switch(context.Background(), f.run, f.st, f.r.Dir, f.r.Dir, "feature/b", fixedNow)
	data, err := os.ReadFile(filepath.Join(f.r.Dir, "gen.txt"))
	if err != nil || strings.TrimSpace(string(data)) != "mine" {
		t.Fatalf("ignored file was lost: %q %v (res %+v)", data, err, res)
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
	res, err := Switch(context.Background(), f.run, st, f.r.Dir, f.r.Dir, "feature/b", fixedNow)
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
	res, err := Switch(context.Background(), f.run, f.st, f.r.Dir, wt, "feature/e", fixedNow)
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
