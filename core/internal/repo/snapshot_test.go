package repo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/testrepo"
)

func findWorktree(t *testing.T, s Snapshot, suffix string) Worktree {
	t.Helper()
	for _, w := range s.Worktrees {
		if strings.HasSuffix(w.Path, suffix) {
			return w
		}
	}
	t.Fatalf("no worktree ending in %q: %+v", suffix, s.Worktrees)
	return Worktree{}
}

func findRef(refs []Ref, name string) (Ref, bool) {
	for _, r := range refs {
		if r.Name == name {
			return r, true
		}
	}
	return Ref{}, false
}

func TestReadBasics(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	r.Branch("feature/a", "main")
	wt := r.AddWorktree("a", "feature/a")
	if err := os.WriteFile(wt+"/README", []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wt+"/new.txt", []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	s, err := Read(ctx, gitx.ExecRunner{}, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Worktrees) != 2 {
		t.Fatalf("worktrees = %d, want 2: %+v", len(s.Worktrees), s.Worktrees)
	}
	if !findWorktree(t, s, "/work").Main {
		t.Error("main worktree not marked Main")
	}
	a := findWorktree(t, s, "/wt-a")
	if a.Branch != "feature/a" || a.Dirty.Changed != 1 || a.Dirty.Untracked != 1 {
		t.Errorf("wt-a = %+v", a)
	}
	if s.RemoteHead != "main" {
		t.Errorf("RemoteHead = %q", s.RemoteHead)
	}
	fa, ok := findRef(s.Local, "feature/a")
	if !ok || fa.Upstream != "" {
		t.Errorf("feature/a = %+v ok=%v", fa, ok)
	}
	if _, ok := findRef(s.Remote, "origin/main"); !ok {
		t.Errorf("remote refs = %+v", s.Remote)
	}
	if _, ok := findRef(s.Remote, "origin/HEAD"); ok {
		t.Error("origin/HEAD must be dropped")
	}

	r.Push(wt, "-u", "origin", "feature/a")
	s, err = Read(ctx, gitx.ExecRunner{}, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	fa, _ = findRef(s.Local, "feature/a")
	if fa.Upstream != "refs/remotes/origin/feature/a" || fa.UpstreamRemoteRef != "refs/heads/feature/a" || fa.Ahead != 0 || fa.Behind != 0 {
		t.Errorf("after push feature/a = %+v", fa)
	}
	// A local branch named like the remote-tracking one makes the short
	// upstream name ambiguous; the full name is not.
	r.Branch("origin/feature/a", "main")
	s, _ = Read(ctx, gitx.ExecRunner{}, r.Dir)
	if fa, _ = findRef(s.Local, "feature/a"); fa.Upstream != "refs/remotes/origin/feature/a" {
		t.Errorf("with a shadowing local branch feature/a = %+v", fa)
	}
	r.Git(r.Dir, "branch", "-D", "origin/feature/a")
	r.Git(r.Dir, "branch", "--track", "alice/x", "origin/main")
	s, _ = Read(ctx, gitx.ExecRunner{}, r.Dir)
	if ax, _ := findRef(s.Local, "alice/x"); ax.Upstream != "refs/remotes/origin/main" || ax.UpstreamRemoteRef != "refs/heads/main" {
		t.Errorf("alice/x = %+v", ax)
	}
	r.Commit(wt, "more", map[string]string{"more.txt": "x\n"})
	s, _ = Read(ctx, gitx.ExecRunner{}, r.Dir)
	fa, _ = findRef(s.Local, "feature/a")
	if fa.Ahead != 1 {
		t.Errorf("ahead = %d, want 1", fa.Ahead)
	}
}

func TestReadPathWithSpacesAndChinese(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	r.Branch("feature/b", "main")
	p := r.Root + "/wt 中文"
	r.Git(r.Dir, "worktree", "add", p, "feature/b")
	if err := os.WriteFile(p+"/文件 1.txt", []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Read(context.Background(), gitx.ExecRunner{}, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	w := findWorktree(t, s, "wt 中文")
	if w.Branch != "feature/b" || w.Dirty.Untracked != 1 {
		t.Errorf("worktree = %+v", w)
	}
	if len(w.Dirty.Files) != 1 || w.Dirty.Files[0] != "文件 1.txt" {
		t.Errorf("files = %q", w.Dirty.Files)
	}
}

func TestReadDetachedWorktree(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	p := r.Root + "/wt-det"
	r.Git(r.Dir, "worktree", "add", "--detach", p)
	s, err := Read(context.Background(), gitx.ExecRunner{}, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	w := findWorktree(t, s, "/wt-det")
	if !w.Detached || w.Branch != "" || w.Head == "" {
		t.Errorf("worktree = %+v", w)
	}
}

func TestReadLockedWorktree(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	r.Branch("feature/l", "main")
	r.Branch("feature/m", "main")
	wl := r.AddWorktree("l", "feature/l")
	r.AddWorktree("m", "feature/m")
	r.Git(r.Dir, "worktree", "lock", "--reason", "on a stick", wl)
	s, err := Read(context.Background(), gitx.ExecRunner{}, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if w := findWorktree(t, s, "/wt-l"); !w.Locked {
		t.Errorf("locked worktree = %+v", w)
	}
	if w := findWorktree(t, s, "/wt-m"); w.Locked {
		t.Errorf("unlocked worktree = %+v", w)
	}
}

func TestReadMissingWorktree(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	r.Branch("feature/c", "main")
	wt := r.AddWorktree("c", "feature/c")
	if err := os.RemoveAll(filepath.FromSlash(wt)); err != nil {
		t.Fatal(err)
	}
	s, err := Read(context.Background(), gitx.ExecRunner{}, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	w := findWorktree(t, s, "/wt-c")
	if !w.Missing || w.Dirty.Changed != 0 {
		t.Errorf("worktree = %+v", w)
	}
}

func TestReadUpstreamGone(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	r.Branch("feature/b", "main")
	r.Push(r.Dir, "-u", "origin", "feature/b")
	r.Git(r.Dir, "push", "origin", "--delete", "feature/b")
	r.Git(r.Dir, "fetch", "--prune")
	s, err := Read(context.Background(), gitx.ExecRunner{}, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	fb, ok := findRef(s.Local, "feature/b")
	if !ok || !fb.UpstreamGone {
		t.Errorf("feature/b = %+v ok=%v", fb, ok)
	}
}

func TestDiscover(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	r.Branch("feature/a", "main")
	wt := r.AddWorktree("a", "feature/a")
	got, err := Discover(context.Background(), gitx.ExecRunner{}, wt)
	if err != nil {
		t.Fatal(err)
	}
	if got != r.Dir {
		t.Errorf("Discover = %q, want %q", got, r.Dir)
	}
	_, err = Discover(context.Background(), gitx.ExecRunner{}, filepath.ToSlash(t.TempDir()))
	if !errors.Is(err, ErrNotRepo) {
		t.Errorf("err = %v, want ErrNotRepo", err)
	}
}

func TestParseStatusRenameSkipsOriginalPath(t *testing.T) {
	out := "2 R. N... 100644 100644 100644 aaa bbb R100 new name.txt\x00old name.txt\x00" +
		"1 .M N... 100644 100644 100644 aaa bbb a.txt\x00? u.txt\x00"
	d := parseStatus(out)
	if d.Changed != 2 || d.Untracked != 1 {
		t.Fatalf("counts = %+v", d)
	}
	want := []string{"new name.txt", "a.txt", "u.txt"}
	if strings.Join(d.Files, "|") != strings.Join(want, "|") {
		t.Errorf("files = %q", d.Files)
	}
}

// A submodule is a repository of its own: its root is its own folder, not the
// superproject's git directory.
func TestDiscoverSubmodule(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	r.Git(r.Dir, "-c", "protocol.file.allow=always", "submodule", "add", r.Remote, "sub")
	sub := r.Dir + "/sub"
	got, err := Discover(context.Background(), gitx.ExecRunner{}, sub)
	if err != nil {
		t.Fatal(err)
	}
	if !sameDir(t, got, sub) {
		t.Fatalf("Discover(submodule) = %q, want %q", got, sub)
	}
	if got, err := Discover(context.Background(), gitx.ExecRunner{}, r.Dir); err != nil || !sameDir(t, got, r.Dir) {
		t.Fatalf("Discover(superproject) = %q, %v", got, err)
	}
}

func sameDir(t *testing.T, a, b string) bool {
	t.Helper()
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		t.Fatalf("EvalSymlinks: %v %v", err1, err2)
	}
	return strings.EqualFold(filepath.Clean(ra), filepath.Clean(rb))
}

func TestDiscoverBareRepo(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	if _, err := Discover(context.Background(), gitx.ExecRunner{}, r.Remote); !errors.Is(err, ErrBareRepo) {
		t.Fatalf("err = %v, want ErrBareRepo", err)
	}
}

// failRunner fails every command with err.
type failRunner struct {
	gitx.ExecRunner
	err error
}

func (f failRunner) Run(context.Context, string, ...string) ([]byte, error) { return nil, f.err }

// git refuses a repository owned by another user ("dubious ownership"): that
// is not "not a repository", and git's text tells the user what to do.
func TestDiscoverUnsafeRepo(t *testing.T) {
	stderr := "fatal: detected dubious ownership in repository at 'D:/x'\n'D:/x' is owned by:\n\tsomeone\n" +
		"To add an exception for this directory, call:\n\n\tgit config --global --add safe.directory D:/x\n"
	run := failRunner{err: &gitx.GitError{Args: []string{"worktree"}, ExitCode: 128, Stderr: stderr}}
	_, err := Discover(context.Background(), run, t.TempDir())
	var ue *UnsafeRepoError
	if !errors.As(err, &ue) || !strings.Contains(ue.Message, "dubious ownership") || !strings.Contains(ue.Message, "safe.directory") {
		t.Fatalf("err = %v, want UnsafeRepoError with git's text", err)
	}
	if errors.Is(err, ErrNotRepo) {
		t.Fatal("an unsafe repository is not reported as no repository")
	}
}

// A folder git cannot read (its .git file points nowhere) must not break the
// whole snapshot; it is marked missing and broken.
func TestReadBrokenWorktree(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	r.Branch("feature/b", "main")
	wt := r.AddWorktree("b", "feature/b")
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+r.Root+"/nowhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Read(context.Background(), gitx.ExecRunner{}, r.Dir)
	if err != nil {
		t.Fatalf("Read failed for one broken folder: %v", err)
	}
	if w := findWorktree(t, s, "/wt-b"); !w.Missing || !w.Broken {
		t.Errorf("broken worktree = %+v", w)
	}
	if w := findWorktree(t, s, "/work"); w.Missing || w.Broken {
		t.Errorf("main worktree = %+v", w)
	}
}

// The snapshot of a submodule lists its real folder as the main folder, not
// the git directory git names first.
func TestReadSubmodule(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	r.Git(r.Dir, "-c", "protocol.file.allow=always", "submodule", "add", r.Remote, "sub")
	sub := r.Dir + "/sub"
	s, err := Read(context.Background(), gitx.ExecRunner{}, sub)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Worktrees) == 0 || !s.Worktrees[0].Main || !sameDir(t, s.Worktrees[0].Path, sub) {
		t.Fatalf("worktrees = %+v, want the main folder %s", s.Worktrees, sub)
	}
}
