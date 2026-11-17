package ops

import (
	"context"
	"os"
	"path/filepath"
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
