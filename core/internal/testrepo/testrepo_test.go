package testrepo

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func commitTime(t *testing.T, r *Repo, dir, rev string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(r.Git(dir, "log", "-1", "--format=%ct", rev), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestNew(t *testing.T) {
	r := New(t)
	if r.Git(r.Dir, "rev-parse", "origin/main") != r.Git(r.Dir, "rev-parse", "main") {
		t.Fatal("origin/main and main differ")
	}
	if got := r.Git(r.Dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); got != "origin/main" {
		t.Fatalf("origin/HEAD = %q", got)
	}
	h1 := r.Commit(r.Dir, "one", map[string]string{"a.txt": "1"})
	h2 := r.Commit(r.Dir, "two", map[string]string{"a.txt": "2"})
	if d := commitTime(t, r, r.Dir, h2) - commitTime(t, r, r.Dir, h1); d != 3600 {
		t.Fatalf("commit time gap = %d", d)
	}
}

func TestCommitAt(t *testing.T) {
	r := New(t)
	before := r.Clock()
	at := time.Date(2020, 5, 6, 7, 8, 9, 0, time.UTC)
	h := r.CommitAt(r.Dir, "old", map[string]string{"b.txt": "x"}, at)
	if got := commitTime(t, r, r.Dir, h); got != at.Unix() {
		t.Fatalf("ct = %d, want %d", got, at.Unix())
	}
	if !r.Clock().Equal(before) {
		t.Fatal("CommitAt advanced the clock")
	}
}

func TestBranchPushWorktreeClone(t *testing.T) {
	r := New(t)
	r.Branch("feature/login", "main")
	r.Git(r.Dir, "checkout", "feature/login")
	r.Commit(r.Dir, "f", map[string]string{"f.txt": "f"})
	r.Push(r.Dir, "origin", "feature/login")
	if !strings.Contains(r.Git(r.Dir, "ls-remote", "--heads", "origin"), "refs/heads/feature/login") {
		t.Fatal("branch not pushed")
	}
	r.Git(r.Dir, "checkout", "main")
	wt := r.AddWorktree("v2", "feature/login")
	if wt != r.Root+"/wt-v2" || strings.Contains(wt, "\\") {
		t.Fatalf("worktree path %q", wt)
	}
	if r.Git(wt, "rev-parse", "--abbrev-ref", "HEAD") != "feature/login" {
		t.Fatal("worktree branch wrong")
	}
	c := r.Clone("other")
	r.Commit(c, "theirs", map[string]string{"t.txt": "t"})
	r.Push(c, "origin", "main")
	r.Git(r.Dir, "fetch", "origin")
	if r.Git(r.Dir, "rev-parse", "origin/main") == r.Git(r.Dir, "rev-parse", "main") {
		t.Fatal("clone push not visible")
	}
}

func TestApplyEnv(t *testing.T) {
	r := New(t)
	r.ApplyEnv()
	if os.Getenv("HOME") != r.Root+"/home" || os.Getenv("GIT_CONFIG_NOSYSTEM") != "1" {
		t.Fatal("env not applied")
	}
	if os.Getenv("GIT_CONFIG_GLOBAL") != r.Root+"/home/.gitconfig" {
		t.Fatal("global config not applied")
	}
}
