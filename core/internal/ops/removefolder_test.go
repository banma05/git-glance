package ops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/model"
	"github.com/banma05/git-glance/core/internal/repo"
	"github.com/banma05/git-glance/core/internal/settings"
)

// removeFixture has a merged, pushed, clean branch alice/done in its own
// folder, with ignored files .env, node_modules/ and notes/.
type removeFixture struct {
	*fixture
	wt string
}

func newRemoveFixture(t *testing.T) *removeFixture {
	t.Helper()
	f := newFixture(t)
	f.r.Commit(f.r.Dir, "ignore", map[string]string{".gitignore": ".env\nnode_modules/\nnotes/\n"})
	f.r.Push(f.r.Dir, "origin", "main")
	f.r.Branch("alice/done", "main")
	f.r.Push(f.r.Dir, "-u", "origin", "alice/done")
	wt := f.r.AddWorktree("done", "alice/done")
	return &removeFixture{fixture: f, wt: wt}
}

func (f *removeFixture) write(t *testing.T, rel, content string) {
	t.Helper()
	p := filepath.Join(f.wt, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *removeFixture) view(t *testing.T) model.View {
	t.Helper()
	ctx := context.Background()
	ver, err := gitx.DetectVersion(ctx, f.run)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := repo.Read(ctx, f.run, f.r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	rs := settings.RepoSettings{Trunks: []settings.TrunkConfig{{Name: "main"}}}
	v, err := model.Build(ctx, f.run, ver, snap, rs, fixedNow().Unix())
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (f *removeFixture) remove(t *testing.T, v model.View, branch string) RemoveFolderResult {
	t.Helper()
	res, err := RemoveFolder(context.Background(), f.run, f.st, v, branch, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if res.Files == nil || res.DeletedBuild == nil || res.SkippedLinks == nil {
		t.Fatalf("nil slice in %+v", res)
	}
	return res
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (f *removeFixture) branchExists() bool {
	return exec.Command("git", "-C", f.r.Dir, "rev-parse", "--verify", "refs/heads/alice/done").Run() == nil
}

func TestRemoveFolderBacksUpIgnored(t *testing.T) {
	f := newRemoveFixture(t)
	f.write(t, ".env", "SECRET=1\n")
	f.write(t, "node_modules/x.js", "module.exports = 1\n")
	f.write(t, "notes/me.txt", "my notes\n")
	f.write(t, "notes/build/plan.md", "plan\n")
	res := f.remove(t, f.view(t), "alice/done")
	if !res.OK || res.Reason != "" || res.LogError != "" {
		t.Fatalf("res = %+v, want ok", res)
	}
	if exists(f.wt) {
		t.Fatal("folder still exists")
	}
	if !f.branchExists() {
		t.Fatal("the branch must survive")
	}
	for rel, want := range map[string]string{".env": "SECRET=1\n", "notes/me.txt": "my notes\n", "notes/build/plan.md": "plan\n"} {
		if got := readFile(t, filepath.Join(res.BackupDir, filepath.FromSlash(rel))); got != want {
			t.Fatalf("backup of %s = %q", rel, got)
		}
	}
	if exists(filepath.Join(res.BackupDir, "node_modules")) {
		t.Fatal("build output was backed up")
	}
	if len(res.DeletedBuild) != 1 || res.DeletedBuild[0] != "node_modules/" {
		t.Fatalf("DeletedBuild = %v", res.DeletedBuild)
	}
	if want := int64(len("SECRET=1\n") + len("my notes\n") + len("plan\n")); res.BackupBytes != want {
		t.Fatalf("BackupBytes = %d, want %d", res.BackupBytes, want)
	}
	if len(res.Files) != 3 {
		t.Fatalf("Files = %v", res.Files)
	}
	if got := f.opNames(t); len(got) != 1 || got[0] != "remove-folder" {
		t.Fatalf("ops = %v", got)
	}
	ops, _ := f.st.Ops(f.r.Dir, 1)
	if ops[0].Detail != res.BackupDir || ops[0].Branch != "alice/done" {
		t.Fatalf("op = %+v", ops[0])
	}
}

func TestRemoveFolderNothingToBackUp(t *testing.T) {
	f := newRemoveFixture(t)
	f.write(t, "node_modules/x.js", "x\n")
	res := f.remove(t, f.view(t), "alice/done")
	if !res.OK || res.BackupDir != "" || res.BackupBytes != 0 {
		t.Fatalf("res = %+v", res)
	}
	if exists(filepath.Join(f.st.Dir, "backups")) {
		t.Fatal("a backup directory was created for nothing")
	}
}

func TestRemoveFolderRefusesMainFolder(t *testing.T) {
	f := newRemoveFixture(t)
	f.r.Branch("feature/m", "main")
	f.r.Git(f.r.Dir, "checkout", "feature/m")
	res := f.remove(t, f.view(t), "feature/m")
	if res.OK || res.Reason != "main-folder" {
		t.Fatalf("res = %+v", res)
	}
	if !exists(f.r.Dir) {
		t.Fatal("main folder is gone")
	}
}

func TestRemoveFolderRefusesDirty(t *testing.T) {
	f := newRemoveFixture(t)
	f.write(t, "README", "changed\n")
	res := f.remove(t, f.view(t), "alice/done")
	if res.OK || res.Reason != "dirty" || !exists(f.wt) {
		t.Fatalf("res = %+v", res)
	}
}

func TestRemoveFolderMissingFolder(t *testing.T) {
	f := newRemoveFixture(t)
	if err := os.RemoveAll(f.wt); err != nil {
		t.Fatal(err)
	}
	res := f.remove(t, f.view(t), "alice/done")
	if res.OK || res.Reason != "folder-missing" {
		t.Fatalf("res = %+v", res)
	}
}

func TestRemoveFolderUnknownBranch(t *testing.T) {
	f := newRemoveFixture(t)
	if _, err := RemoveFolder(context.Background(), f.run, f.st, f.view(t), "nope", fixedNow); err == nil {
		t.Fatal("want an error for a branch outside the view")
	}
}

func TestRemoveFolderChanged(t *testing.T) {
	f := newRemoveFixture(t)
	v := f.view(t)
	f.r.Git(f.wt, "checkout", "--detach")
	f.r.Commit(f.wt, "only here", map[string]string{"new.txt": "n\n"})
	res := f.remove(t, v, "alice/done")
	if res.OK || res.Reason != "changed" {
		t.Fatalf("res = %+v, want changed", res)
	}
	if !exists(filepath.Join(f.wt, "new.txt")) {
		t.Fatal("the folder was touched")
	}
	if len(f.opNames(t)) != 0 {
		t.Fatal("a refused removal must not be logged")
	}
}

func TestRemoveFolderProbeNameTaken(t *testing.T) {
	f := newRemoveFixture(t)
	probe := f.wt + lockProbeSuffix
	if err := os.Mkdir(probe, 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := RemoveFolder(context.Background(), f.run, f.st, f.view(t), "alice/done", fixedNow)
	if err == nil || res.OK {
		t.Fatalf("res = %+v, err = %v, want an error", res, err)
	}
	if !exists(f.wt) || !exists(probe) {
		t.Fatal("something was moved")
	}
}

func TestRemoveFolderBackupFailureLeavesFolder(t *testing.T) {
	f := newRemoveFixture(t)
	f.write(t, ".env", "SECRET=1\n")
	v := f.view(t)
	// A file where the backup directory's parent must be makes the copy fail.
	if err := os.WriteFile(filepath.Join(f.st.Dir, "backups"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := f.remove(t, v, "alice/done")
	if res.OK || res.Reason != "backup-failed" || res.BackupDir != "" {
		t.Fatalf("res = %+v", res)
	}
	if !exists(filepath.Join(f.wt, ".env")) {
		t.Fatal("the folder was touched")
	}
	if len(f.opNames(t)) != 0 {
		t.Fatal("a failed backup must not be logged as a removal")
	}
}

func TestRemoveFolderExistingBackupDirIsKept(t *testing.T) {
	f := newRemoveFixture(t)
	f.write(t, ".env", "SECRET=1\n")
	v := f.view(t)
	dir := f.st.BackupDir(v.Repo, filepath.Base(f.wt), fixedNow())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := f.remove(t, v, "alice/done")
	if res.OK || res.Reason != "backup-failed" {
		t.Fatalf("res = %+v", res)
	}
	if readFile(t, filepath.Join(dir, "old.txt")) != "old" || !exists(f.wt) {
		t.Fatal("an existing backup was touched or the folder was removed")
	}
}

func TestRemoveFolderLocked(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("a folder can only be locked by an open file on Windows")
	}
	f := newRemoveFixture(t)
	f.write(t, ".env", "SECRET=1\n")
	held, err := os.Open(filepath.Join(f.wt, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	res := f.remove(t, f.view(t), "alice/done")
	if res.OK || res.Reason != "locked" {
		t.Fatalf("res = %+v, want locked", res)
	}
	if !exists(filepath.Join(f.wt, ".env")) || exists(f.wt+lockProbeSuffix) {
		t.Fatal("the folder was changed")
	}
	if exists(filepath.Join(f.st.Dir, "backups")) {
		t.Fatal("a backup directory was created")
	}
}

// junction makes link point at target with a directory junction (no admin).
func junction(t *testing.T, link, target string) {
	t.Helper()
	out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Fatalf("mklink: %v: %s", err, out)
	}
}

func TestRemoveFolderDoesNotFollowJunction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("directory junctions exist only on Windows")
	}
	f := newRemoveFixture(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.write(t, "node_modules/x.js", "x\n")
	f.write(t, "notes/me.txt", "my notes\n")
	junction(t, filepath.Join(f.wt, "node_modules", "link"), outside)
	junction(t, filepath.Join(f.wt, "notes", "link"), outside)
	res := f.remove(t, f.view(t), "alice/done")
	if !res.OK {
		t.Fatalf("res = %+v", res)
	}
	if readFile(t, filepath.Join(outside, "keep.txt")) != "keep\n" {
		t.Fatal("the file behind the junction was changed")
	}
	if exists(f.wt) {
		t.Fatal("folder still exists")
	}
	if len(res.SkippedLinks) != 1 || res.SkippedLinks[0] != "notes/link" {
		t.Fatalf("SkippedLinks = %v", res.SkippedLinks)
	}
	if readFile(t, filepath.Join(res.BackupDir, "notes", "me.txt")) != "my notes\n" {
		t.Fatal("the plain file was not backed up")
	}
	found := false
	_ = filepath.WalkDir(res.BackupDir, func(p string, d os.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(p, "keep.txt") {
			found = true
		}
		return nil
	})
	if found {
		t.Fatal("content behind a junction reached the backup")
	}
}
