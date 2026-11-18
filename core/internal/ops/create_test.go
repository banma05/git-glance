package ops

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCreateFolder(t *testing.T) {
	f := newFixture(t)
	p := DefaultFolderPath(f.r.Dir, "alice/feature/new")
	res, err := CreateFolder(context.Background(), f.run, f.st, f.r.Dir, "main", "refs/remotes/origin/main", "alice/feature/new", p, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Path != p || res.Files == nil || res.LogError != "" {
		t.Fatalf("res = %+v", res)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
	if up := f.r.Git(f.r.Dir, "for-each-ref", "--format=%(upstream)", "refs/heads/alice/feature/new"); up != "" {
		t.Fatalf("upstream = %q, want none", up)
	}
	if got := f.head(p); got != "refs/heads/alice/feature/new" {
		t.Fatalf("new folder HEAD = %q", got)
	}
	rs, err := f.st.Repo(f.r.Dir)
	if err != nil || rs.CreatedBase["alice/feature/new"] != "main" {
		t.Fatalf("CreatedBase = %v, err = %v", rs.CreatedBase, err)
	}
	if names := f.opNames(t); len(names) != 1 || names[0] != "create" {
		t.Fatalf("ops = %v, want [create]", names)
	}
}

func TestCreateFolderRefuses(t *testing.T) {
	f := newFixture(t)
	f.r.Git(f.other, "push", "origin", "main:theirs")
	f.r.Git(f.r.Dir, "fetch", "origin")
	f.r.Branch("feature/old", "main")
	wt := f.r.AddWorktree("old", "feature/old")
	taken := f.r.Root + "/taken"
	if err := os.WriteFile(taken, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	free := f.r.Root + "/free"
	cases := []struct {
		name, branch, path, reason, wantPath string
	}{
		{"local branch", "feature/old", free, "branch-exists", ""},
		{"remote branch", "theirs", free, "branch-exists", ""},
		{"path is a file", "feature/new", taken, "path-exists", taken},
		{"path is a folder", "feature/new", f.r.Root, "path-exists", f.r.Root},
		{"bad name", "bad..name", free, "bad-name", ""},
		{"reflog shortcut", "@{-1}", free, "bad-name", ""},
		{"inside the main folder", "feature/new", f.r.Dir + "/sub/x", "path-inside-folder", f.r.Dir},
		{"inside another folder", "feature/new", wt + "/x", "path-inside-folder", wt},
		{"the folder itself", "feature/new", wt, "path-inside-folder", wt},
	}
	for _, c := range cases {
		res, err := CreateFolder(context.Background(), f.run, f.st, f.r.Dir, "main", "refs/remotes/origin/main", c.branch, c.path, fixedNow)
		if err != nil || res.OK || res.Reason != c.reason || res.Files == nil {
			t.Errorf("%s: res = %+v, err = %v, want reason %s", c.name, res, err, c.reason)
			continue
		}
		if c.wantPath != "" && filepath.ToSlash(res.Path) != c.wantPath {
			t.Errorf("%s: Path = %q, want %q", c.name, res.Path, c.wantPath)
		}
	}
	if _, err := os.Stat(free); err == nil {
		t.Fatal("a refused create left a folder behind")
	}
	if len(f.opNames(t)) != 0 {
		t.Fatal("refusals must not be logged")
	}
}

func TestDefaultFolderPathSanitizes(t *testing.T) {
	if got := DefaultFolderPath("D:/work/app", "alice/fix:a*b?"); got != "D:/work/app-wt-fix-a-b-" {
		t.Fatalf("got %q", got)
	}
	if got := DefaultFolderPath("D:/work/app/", "x y\"<>|\\z"); got != "D:/work/app-wt-x-y-----z" {
		t.Fatalf("got %q", got)
	}
}

func TestCreateFolderNameCollisions(t *testing.T) {
	f := newFixture(t)
	f.r.Git(f.r.Dir, "switch", "-c", "alice/x")
	f.r.Commit(f.r.Dir, "unpushed", map[string]string{"mine": "1\n"})
	f.r.Git(f.r.Dir, "switch", "main")
	f.r.Git(f.r.Dir, "pack-refs", "--all")
	aliceBefore := f.rev("refs/heads/alice/x")
	cases := []struct{ branch, reason string }{
		{"Alice/x", "branch-exists"},
		{"ALICE/X", "branch-exists"},
		{"X", "branch-exists"},
		{"origin/main", "bad-name"},
		{"Origin/Main", "bad-name"},
		{"origin/new-thing", "bad-name"},
		{"origin", "bad-name"},
	}
	for _, c := range cases {
		p := f.r.Root + "/wt-new"
		res, err := CreateFolder(context.Background(), f.run, f.st, f.r.Dir, "main", "refs/heads/main", c.branch, p, fixedNow)
		if err != nil || res.OK || res.Reason != c.reason {
			t.Errorf("%s: res = %+v, err = %v, want %s", c.branch, res, err, c.reason)
		}
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s: a folder was created", c.branch)
		}
		if got := f.r.Git(f.r.Dir, "for-each-ref", "--format=%(refname)", "refs/heads/"+c.branch); got != "" && c.branch != "X" && c.branch != "Alice/x" && c.branch != "ALICE/X" {
			t.Errorf("%s: a branch exists afterwards: %s", c.branch, got)
		}
	}
	if f.rev("refs/heads/alice/x") != aliceBefore {
		t.Fatal("an existing branch moved")
	}
	p := f.r.Root + "/wt-origin-fix"
	res, err := CreateFolder(context.Background(), f.run, f.st, f.r.Dir, "main", "refs/heads/main", "origin-fix/x", p, fixedNow)
	if err != nil || !res.OK {
		t.Fatalf("origin-fix/x: res = %+v, err = %v, want ok", res, err)
	}
}

// git creates the branch before it checks out the folder. When the rest fails
// the branch stays (it holds nothing but the start commit): report and log it
// with its hash, never delete it.
func TestCreateFolderFailureReportsTheBranchLeftBehind(t *testing.T) {
	f := newFixture(t)
	hook := filepath.Join(f.r.Dir, ".git", "hooks", "post-checkout")
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := f.r.Root + "/wt-hooked"
	res, err := CreateFolder(context.Background(), f.run, f.st, f.r.Dir, "main", "refs/remotes/origin/main", "feature/hooked", p, fixedNow)
	if err != nil || res.OK || res.Reason != "git-failed" || res.Files == nil {
		t.Fatalf("res = %+v, err = %v, want git-failed", res, err)
	}
	hash := f.rev("refs/heads/feature/hooked")
	if !strings.Contains(res.Detail, "feature/hooked") || !strings.Contains(res.Detail, hash) {
		t.Fatalf("detail %q does not name the branch and %s", res.Detail, hash)
	}
	if e, ok := opFor(t, f, "create", "feature/hooked"); !ok || !strings.HasPrefix(e.Result, "git-failed: ") || e.After != hash {
		t.Fatalf("log entry = %+v (found %v)", e, ok)
	}
}

// Names git would read as something else: a tag, a ref namespace, or a
// pseudo-ref such as HEAD or FETCH_HEAD (case-insensitive file systems make
// case variants collide too).
func TestCreateFolderRefusesAmbiguousNames(t *testing.T) {
	f := newFixture(t)
	f.r.Git(f.r.Dir, "tag", "v1")
	f.r.Git(f.r.Dir, "tag", "rel/2.0")
	for i, name := range []string{
		"v1", "V1", "rel/2.0",
		"refs/x", "heads/x", "tags/x", "remotes/x", "Heads/x",
		"HEAD", "head", "ORIG_HEAD", "FETCH_HEAD", "fetch_head", "x_HEAD", "feature/MERGE_HEAD",
	} {
		p := f.r.Root + "/wt-amb-" + strconv.Itoa(i)
		res, err := CreateFolder(context.Background(), f.run, f.st, f.r.Dir, "main", "refs/heads/main", name, p, fixedNow)
		if err != nil || res.OK || res.Reason != "bad-name" {
			t.Errorf("%s: res = %+v, err = %v, want bad-name", name, res, err)
		}
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s: a folder was created", name)
		}
	}
	res, err := CreateFolder(context.Background(), f.run, f.st, f.r.Dir, "main", "refs/heads/main", "feature/v1-heads", f.r.Root+"/wt-ok", fixedNow)
	if err != nil || !res.OK {
		t.Fatalf("a plain name was refused: %+v, %v", res, err)
	}
}

func TestCreateFolderForExistingBranch(t *testing.T) {
	f := newFixture(t)
	before := f.rev("refs/heads/x")
	p := f.r.Root + "/wt-x"
	res, err := CreateFolderForBranch(context.Background(), f.run, f.st, f.r.Dir, "x", p, fixedNow)
	if err != nil || !res.OK || res.Path != p || res.Files == nil || res.LogError != "" {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if f.head(p) != "refs/heads/x" || f.rev("refs/heads/x") != before {
		t.Fatalf("folder on %s, branch at %s (was %s)", f.head(p), f.rev("refs/heads/x"), before)
	}
	if up := f.r.Git(f.r.Dir, "for-each-ref", "--format=%(upstream)", "refs/heads/x"); up != "refs/remotes/origin/x" {
		t.Fatalf("upstream changed to %q", up)
	}
	e, ok := opFor(t, f, "create", "x")
	if !ok || e.Result != "ok" || e.Before != before || e.After != before || strings.Join(e.Command, " ") != "git worktree add "+p+" x" {
		t.Fatalf("log entry = %+v (found %v)", e, ok)
	}
	if rs, _ := f.st.Repo(f.r.Dir); rs.CreatedBase["x"] != "" {
		t.Fatalf("an existing branch got a created base: %v", rs.CreatedBase)
	}
}

// Only a local branch that no folder holds gets a folder: never a branch
// created on the way (git would make one from a same-named remote branch).
func TestCreateFolderForExistingBranchRefuses(t *testing.T) {
	f := newFixture(t)
	f.r.Git(f.other, "push", "origin", "main:theirs")
	f.r.Git(f.r.Dir, "fetch", "origin")
	wy := f.r.AddWorktree("y", "y")
	wz := f.r.AddWorktree("z", "z")
	f.r.Git(wz, "bisect", "start")
	f.r.Git(wz, "switch", "--detach")
	gone := f.r.AddWorktree("w", "w")
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	taken := f.r.Root + "/taken"
	if err := os.WriteFile(taken, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	free := f.r.Root + "/free"
	cases := []struct{ name, branch, path, reason, wantPath string }{
		{"remote only", "theirs", free, "no-branch", ""},
		{"no such branch", "nope", free, "no-branch", ""},
		{"main folder's branch", "main", free, "checked-out-elsewhere", f.r.Dir},
		{"another folder's branch", "y", free, "checked-out-elsewhere", wy},
		{"missing folder's branch", "w", free, "checked-out-elsewhere", gone},
		{"bisect", "z", free, "in-progress", ""},
		{"path exists", "x", taken, "path-exists", taken},
		{"inside a folder", "x", wy + "/sub", "path-inside-folder", wy},
		{"bad name", "@{-1}", free, "bad-name", ""},
	}
	for _, c := range cases {
		res, err := CreateFolderForBranch(context.Background(), f.run, f.st, f.r.Dir, c.branch, c.path, fixedNow)
		if err != nil || res.OK || res.Reason != c.reason || res.Files == nil {
			t.Errorf("%s: res = %+v, err = %v, want %s", c.name, res, err, c.reason)
			continue
		}
		if c.wantPath != "" && filepath.ToSlash(res.Path) != c.wantPath {
			t.Errorf("%s: Path = %q, want %q", c.name, res.Path, c.wantPath)
		}
	}
	if _, err := os.Stat(free); err == nil {
		t.Fatal("a refused create left a folder behind")
	}
	if got := f.r.Git(f.r.Dir, "for-each-ref", "refs/heads/theirs", "refs/heads/nope"); got != "" {
		t.Fatalf("a branch was created: %s", got)
	}
	if len(f.opNames(t)) != 0 {
		t.Fatal("refusals must not be logged")
	}
}

// submoduleFixture adds the remote as submodule "sub" of the main folder and
// returns the submodule's folder.
func submoduleFixture(t *testing.T, f *fixture) string {
	t.Helper()
	f.r.Git(f.r.Dir, "-c", "protocol.file.allow=always", "submodule", "add", f.r.Remote, "sub")
	return f.r.Dir + "/sub"
}

func TestCreateFolderRefusesPathInsideSubmoduleFolder(t *testing.T) {
	f := newFixture(t)
	sub := submoduleFixture(t, f)
	res, err := CreateFolder(context.Background(), f.run, f.st, sub, "main", "refs/remotes/origin/main", "feature/in", sub+"/inner", fixedNow)
	if err != nil || res.OK || res.Reason != "path-inside-folder" {
		t.Fatalf("res = %+v, err = %v, want path-inside-folder", res, err)
	}
	if _, err := os.Stat(sub + "/inner"); err == nil {
		t.Fatal("a folder was created inside the submodule")
	}
}
