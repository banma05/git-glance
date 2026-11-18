package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func newStore(t *testing.T) Store {
	t.Helper()
	t.Setenv("GITGLANCE_HOME", t.TempDir())
	dir, err := Home()
	if err != nil {
		t.Fatal(err)
	}
	return Store{Dir: dir}
}

func TestRepoDefaults(t *testing.T) {
	s := newStore(t)
	rs, err := s.Repo("/some/repo")
	if err != nil {
		t.Fatal(err)
	}
	if rs.OldDays != 30 {
		t.Fatalf("OldDays = %d, want 30", rs.OldDays)
	}
	if rs.Trunks == nil || len(rs.Trunks) != 0 {
		t.Fatalf("Trunks = %#v, want empty non-nil", rs.Trunks)
	}
	if rs.ManualTrunk == nil || rs.CreatedBase == nil {
		t.Fatal("maps must be non-nil")
	}
}

func TestRepoRoundTrip(t *testing.T) {
	s := newStore(t)
	in := RepoSettings{
		Trunks:      []TrunkConfig{{Name: "main", Test: "main-test", Releases: []string{"release/eu"}}},
		HidePrefix:  "alice/",
		OldDays:     30,
		ManualTrunk: map[string]string{},
		CreatedBase: map[string]string{},
	}
	if err := s.SaveRepo("/some/repo", in); err != nil {
		t.Fatal(err)
	}
	out, err := s.Repo("/some/repo")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("got %#v, want %#v", out, in)
	}
}

func TestRepoKeyWindowsCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	if RepoKey("D:/Work/Repo") != RepoKey(`d:\work\repo`) {
		t.Fatal("keys differ")
	}
}

func TestOpsNewestFirst(t *testing.T) {
	s := newStore(t)
	for i := int64(1); i <= 3; i++ {
		if err := s.AppendOp(OpEntry{Time: i, Repo: "/r", Op: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	ops, err := s.Ops("", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 2 || ops[0].Time != 3 || ops[1].Time != 2 {
		t.Fatalf("got %#v", ops)
	}
}

func TestOpsEmptyAndFilter(t *testing.T) {
	s := newStore(t)
	ops, err := s.Ops("", 10)
	if err != nil || ops == nil || len(ops) != 0 {
		t.Fatalf("got %#v, %v", ops, err)
	}
	_ = s.AppendOp(OpEntry{Time: 1, Repo: "/a"})
	_ = s.AppendOp(OpEntry{Time: 2, Repo: "/b"})
	ops, _ = s.Ops("/a", 10)
	if len(ops) != 1 || ops[0].Time != 1 {
		t.Fatalf("got %#v", ops)
	}
}

func TestGlobalDefaults(t *testing.T) {
	s := newStore(t)
	g, err := s.Global()
	if err != nil {
		t.Fatal(err)
	}
	if g.Editor != "code" || g.Theme != "auto" || g.Repos == nil {
		t.Fatalf("got %#v", g)
	}
}

func TestBackups(t *testing.T) {
	s := newStore(t)
	if err := s.SaveGlobal(Global{Repos: []string{}, Editor: "code", Theme: "auto"}); err != nil {
		t.Fatal(err)
	}
	dir := s.BackupDir("/some/repo", "feature", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	if filepath.Base(dir) != "feature-20260102-030405" {
		t.Fatalf("dir = %s", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "a"), []byte("123456"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "b"), []byte("7890"), 0o644)
	n, err := s.BackupsSize()
	if err != nil || n != 10 {
		t.Fatalf("size = %d, %v", n, err)
	}
	if err := s.ClearBackups(); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.BackupsSize(); n != 0 {
		t.Fatalf("size after clear = %d", n)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "global.json")); err != nil {
		t.Fatal(err)
	}
}

// The config directory holds the operation log and backups of ignored files
// (often secrets): it is created readable by its owner only.
func TestStoreDirsArePrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not used on Windows")
	}
	s := Store{Dir: filepath.Join(t.TempDir(), "cfg")}
	if err := s.AppendOp(OpEntry{Op: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRepo("/some/repo", RepoSettings{}); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{s.Dir, filepath.Join(s.Dir, "repos")} {
		info, err := os.Stat(d)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Fatalf("%s has mode %o, want 700", d, perm)
		}
	}
}
