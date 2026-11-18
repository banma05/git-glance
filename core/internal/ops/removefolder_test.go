package ops

import (
	"context"
	"errors"
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
	return newRemoveFixtureWith(t, nil)
}

// newRemoveFixtureWith also tracks the extra files on main before the branch
// is created.
func newRemoveFixtureWith(t *testing.T, extra map[string]string) *removeFixture {
	t.Helper()
	f := newFixture(t)
	files := map[string]string{".gitignore": ".env\nnode_modules/\nnotes/\n"}
	for k, v := range extra {
		files[k] = v
	}
	f.r.Commit(f.r.Dir, "ignore", files)
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
	return f.removeWith(t, f.run, v, branch)
}

// removeWith removes the folder of branch the way the panel asks for it: with
// the folder and HEAD the view showed.
func (f *removeFixture) removeWith(t *testing.T, r gitx.Runner, v model.View, branch string) RemoveFolderResult {
	t.Helper()
	bv := v.Branches[branch]
	res, err := RemoveFolder(context.Background(), r, f.st, v, branch, bv.Folder, bv.FolderHead, fixedNow)
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
	if _, err := RemoveFolder(context.Background(), f.run, f.st, f.view(t), "nope", f.wt, "", fixedNow); err == nil {
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
	v := f.view(t)
	bv := v.Branches["alice/done"]
	res, err := RemoveFolder(context.Background(), f.run, f.st, v, "alice/done", bv.Folder, bv.FolderHead, fixedNow)
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
	// notes/link was skipped by the backup, node_modules/link removed before
	// git ran; each is listed once.
	if strings.Join(res.SkippedLinks, ",") != "notes/link,node_modules/link" {
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

func TestRemoveFolderRefusesHiddenChanges(t *testing.T) {
	for _, flag := range []string{"--assume-unchanged", "--skip-worktree"} {
		f := newRemoveFixture(t)
		f.r.Git(f.wt, "update-index", flag, "README")
		f.write(t, "README", "my edit\n")
		res := f.remove(t, f.view(t), "alice/done")
		if res.OK || res.Reason != "hidden-changes" || len(res.Files) != 1 || res.Files[0] != "README" {
			t.Fatalf("%s: res = %+v", flag, res)
		}
		if readFile(t, filepath.Join(f.wt, "README")) != "my edit\n" {
			t.Fatal("the edit is gone")
		}
	}
}

func TestRemoveFolderSparseEntryIsNotHidden(t *testing.T) {
	f := newRemoveFixture(t)
	f.r.Git(f.wt, "update-index", "--skip-worktree", "README")
	if err := os.Remove(filepath.Join(f.wt, "README")); err != nil {
		t.Fatal(err)
	}
	res := f.remove(t, f.view(t), "alice/done")
	if !res.OK {
		t.Fatalf("res = %+v, want ok", res)
	}
}

func TestRemoveFolderBacksUpFilesInsideBuildNamedDir(t *testing.T) {
	f := newRemoveFixtureWith(t, map[string]string{"build/Dockerfile": "FROM x\n"})
	f.write(t, "build/docker/.env", "TOKEN=1\n")
	res := f.remove(t, f.view(t), "alice/done")
	if !res.OK || len(res.DeletedBuild) != 0 {
		t.Fatalf("res = %+v", res)
	}
	if readFile(t, filepath.Join(res.BackupDir, "build", "docker", ".env")) != "TOKEN=1\n" {
		t.Fatal("the ignored file was not backed up")
	}
}

func TestRemoveFolderBuildDirsNested(t *testing.T) {
	f := newRemoveFixtureWith(t, map[string]string{
		"app/keep.txt": "k\n", "pkg/keep.txt": "k\n",
		".gitignore": ".env\nnode_modules/\nnotes/\ntarget/\n__pycache__/\n",
	})
	f.write(t, "app/target/a.class", "a\n")
	f.write(t, "pkg/__pycache__/m.pyc", "m\n")
	res := f.remove(t, f.view(t), "alice/done")
	if !res.OK || len(res.DeletedBuild) != 2 || len(res.Files) != 0 {
		t.Fatalf("res = %+v", res)
	}
}

func TestRemoveFolderIgnoredDirListedWithItsFile(t *testing.T) {
	f := newRemoveFixtureWith(t, map[string]string{"src/main.txt": "m\n"})
	f.write(t, "src/local/.env", "A=1\n")
	res := f.remove(t, f.view(t), "alice/done")
	if !res.OK {
		t.Fatalf("res = %+v", res)
	}
	if len(res.Files) != 1 || res.Files[0] != "src/local/.env" {
		t.Fatalf("Files = %v, want the file once", res.Files)
	}
	if readFile(t, filepath.Join(res.BackupDir, "src", "local", ".env")) != "A=1\n" {
		t.Fatal("not backed up")
	}
}

func TestRemoveFolderBuildNamedDirWithIgnoredFiles(t *testing.T) {
	f := newRemoveFixtureWith(t, map[string]string{"deploy/Dockerfile": "FROM x\n"})
	f.write(t, "deploy/build/.env", "TOKEN=1\n")
	f.write(t, "out/.env", "TOKEN=2\n")
	res := f.remove(t, f.view(t), "alice/done")
	if !res.OK || len(res.DeletedBuild) != 0 {
		t.Fatalf("res = %+v", res)
	}
	if readFile(t, filepath.Join(res.BackupDir, "deploy", "build", ".env")) != "TOKEN=1\n" ||
		readFile(t, filepath.Join(res.BackupDir, "out", ".env")) != "TOKEN=2\n" {
		t.Fatal("a secret was not backed up")
	}
	if want := int64(len("TOKEN=1\n") + len("TOKEN=2\n")); res.BackupBytes != want {
		t.Fatalf("BackupBytes = %d, want %d", res.BackupBytes, want)
	}
}

func TestRemoveFolderIgnoredBuildDirListedAloneStaysBuild(t *testing.T) {
	f := newRemoveFixture(t)
	f.write(t, "node_modules/.env", "T=1\n")
	f.write(t, "node_modules/x.js", "x\n")
	res := f.remove(t, f.view(t), "alice/done")
	if !res.OK || len(res.DeletedBuild) != 1 || res.DeletedBuild[0] != "node_modules/" || res.BackupDir != "" {
		t.Fatalf("res = %+v", res)
	}
}

func TestDropNestedPaths(t *testing.T) {
	got := dropNested([]string{"a/", "a/b", "c/d/", "c/d/e/", "f"})
	if strings.Join(got, ",") != "a/,c/d/,f" {
		t.Fatalf("got %v", got)
	}
}

func TestRemoveFolderGitFailed(t *testing.T) {
	f := newRemoveFixture(t)
	f.write(t, ".env", "SECRET=1\n")
	// Locked after the view was built, so only git notices.
	v := f.view(t)
	f.r.Git(f.r.Dir, "worktree", "lock", f.wt)
	res := f.remove(t, v, "alice/done")
	if res.OK || res.Reason != "git-failed" || !strings.Contains(res.Detail, "lock") || !strings.Contains(res.Detail, "still exists") {
		t.Fatalf("res = %+v", res)
	}
	if res.BackupDir == "" || readFile(t, filepath.Join(res.BackupDir, ".env")) != "SECRET=1\n" {
		t.Fatal("the backup was not kept")
	}
	if !exists(filepath.Join(f.wt, ".env")) {
		t.Fatal("the folder is gone")
	}
	ops, _ := f.st.Ops(f.r.Dir, 1)
	if len(ops) != 1 || !strings.HasPrefix(ops[0].Result, "git-failed: ") || ops[0].Detail != res.BackupDir {
		t.Fatalf("ops = %+v", ops)
	}
}

func TestRemoveFolderLogError(t *testing.T) {
	f := newRemoveFixture(t)
	if err := os.MkdirAll(filepath.Join(f.st.Dir, "oplog.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}
	res := f.remove(t, f.view(t), "alice/done")
	if !res.OK || res.LogError == "" || exists(f.wt) {
		t.Fatalf("res = %+v", res)
	}
}

// ghostRunner lists one more ignored path than exists, so the copy fails after
// real files were already copied.
type ghostRunner struct{ gitx.ExecRunner }

func (g ghostRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	out, err := g.ExecRunner.Run(ctx, dir, args...)
	if err == nil && len(args) > 0 && args[0] == "ls-files" {
		out = append(out, []byte("zzz-ghost.txt\x00")...)
	}
	return out, err
}

// hookRunner runs git through ExecRunner and calls before, when set, ahead of
// every command.
type hookRunner struct {
	gitx.ExecRunner
	before func(dir string, args []string)
}

func (h hookRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if h.before != nil {
		h.before(dir, args)
	}
	return h.ExecRunner.Run(ctx, dir, args...)
}

func isWorktreeRemove(args []string) bool {
	return len(args) > 1 && args[0] == "worktree" && args[1] == "remove"
}

// Older Git for Windows treats a junction as a directory and deletes the files
// of its target during `worktree remove`: the junction must be gone before git
// runs.
func TestRemoveFolderDropsJunctionsBeforeGit(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("directory junctions exist only on Windows")
	}
	f := newRemoveFixtureWith(t, map[string]string{".gitignore": ".env\nnode_modules/\nnotes/\nbuild/\n"})
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.write(t, "build/out.txt", "x\n")
	link := filepath.Join(f.wt, "build", "dep")
	junction(t, link, outside)
	atRemove := "git did not run"
	run := hookRunner{before: func(_ string, args []string) {
		if isWorktreeRemove(args) {
			atRemove = "present"
			if !exists(link) {
				atRemove = "gone"
			}
		}
	}}
	res := f.removeWith(t, run, f.view(t), "alice/done")
	if !res.OK {
		t.Fatalf("res = %+v", res)
	}
	if atRemove != "gone" {
		t.Fatalf("junction when worktree remove started: %s", atRemove)
	}
	if readFile(t, filepath.Join(outside, "keep.txt")) != "keep\n" {
		t.Fatal("the file behind the junction was changed")
	}
	found := false
	for _, s := range res.SkippedLinks {
		found = found || s == "build/dep"
	}
	if !found {
		t.Fatalf("SkippedLinks = %v, want build/dep", res.SkippedLinks)
	}
}

// A build-named directory that is itself a repository (a gh-pages checkout in
// dist/) can hold unpushed commits: never delete it as build output.
func TestRemoveFolderRefusesNestedRepoInBuildDir(t *testing.T) {
	f := newRemoveFixtureWith(t, map[string]string{".gitignore": ".env\nnode_modules/\nnotes/\ndist/\n"})
	dist := filepath.Join(f.wt, "dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}
	f.r.Git(dist, "init")
	f.r.Commit(dist, "pages", map[string]string{"index.html": "pages\n"})
	f.write(t, "node_modules/pkg/.git", "gitdir: elsewhere\n")
	res := f.remove(t, f.view(t), "alice/done")
	if res.OK || res.Reason != "nested-repo" {
		t.Fatalf("res = %+v, want nested-repo", res)
	}
	if strings.Join(res.Files, ",") != "dist/.git,node_modules/pkg/.git" {
		t.Fatalf("Files = %v", res.Files)
	}
	if !exists(filepath.Join(dist, "index.html")) || !f.branchExists() {
		t.Fatal("the folder or the branch was touched")
	}
	if exists(filepath.Join(f.st.Dir, "backups")) || len(f.opNames(t)) != 0 {
		t.Fatal("a refused removal made a backup or a log entry")
	}
}

func TestRemoveFolderPartialBackupIsRemoved(t *testing.T) {
	f := newRemoveFixture(t)
	f.write(t, ".env", "SECRET=1\n")
	f.write(t, "notes/me.txt", "n\n")
	v := f.view(t)
	res := f.removeWith(t, ghostRunner{}, v, "alice/done")
	if res.OK || res.Reason != "backup-failed" || res.BackupDir != "" {
		t.Fatalf("res = %+v", res)
	}
	if exists(f.st.BackupDir(v.Repo, filepath.Base(f.wt), fixedNow())) {
		t.Fatal("a partial backup is left")
	}
	if !exists(filepath.Join(f.wt, ".env")) || !exists(filepath.Join(f.wt, "notes", "me.txt")) {
		t.Fatal("the folder was touched")
	}
}

// The panel quotes the folder and HEAD it showed; anything else is a change
// the user has not seen.
func TestRemoveFolderRefusesStaleFolderOrHead(t *testing.T) {
	f := newRemoveFixture(t)
	v := f.view(t)
	bv := v.Branches["alice/done"]
	for _, c := range []struct{ name, folder, head string }{
		{"other folder", f.r.Root + "/wt-other", bv.FolderHead},
		{"other head", bv.Folder, strings.Repeat("0", 40)},
	} {
		res, err := RemoveFolder(context.Background(), f.run, f.st, v, "alice/done", c.folder, c.head, fixedNow)
		if err != nil || res.OK || res.Reason != "changed" || res.Detail == "" {
			t.Fatalf("%s: res = %+v, err = %v, want changed", c.name, res, err)
		}
	}
	// A new commit in the folder after the view was built.
	f.r.Commit(f.wt, "late", map[string]string{"late.txt": "late\n"})
	res := f.remove(t, v, "alice/done")
	if res.OK || res.Reason != "changed" || !strings.Contains(res.Detail, bv.FolderHead) {
		t.Fatalf("new commit: res = %+v, want changed", res)
	}
	if !exists(filepath.Join(f.wt, "late.txt")) || len(f.opNames(t)) != 0 || exists(filepath.Join(f.st.Dir, "backups")) {
		t.Fatal("a refused removal touched something")
	}
}

// Anything that changes while the backup is copied must stop the removal: the
// copy would miss it. Nothing is deleted and the backup is dropped.
func TestRemoveFolderChangedDuringBackup(t *testing.T) {
	cases := []struct {
		name   string
		change func(t *testing.T, f *removeFixture)
	}{
		{"new ignored file", func(t *testing.T, f *removeFixture) { f.write(t, "debug.log", "log\n") }},
		{"new file in a backed-up dir", func(t *testing.T, f *removeFixture) { f.write(t, "notes/later.txt", "later\n") }},
		{"backed-up file grew", func(t *testing.T, f *removeFixture) { f.write(t, ".env", "SECRET=1\nMORE=2\n") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newRemoveFixtureWith(t, map[string]string{".gitignore": ".env\nnode_modules/\nnotes/\n*.log\n"})
			f.write(t, ".env", "SECRET=1\n")
			f.write(t, "notes/me.txt", "n\n")
			v := f.view(t)
			listings := 0
			run := hookRunner{before: func(_ string, args []string) {
				if len(args) > 1 && args[0] == "ls-files" && args[1] == "--others" {
					if listings++; listings == 2 {
						c.change(t, f)
					}
				}
			}}
			res := f.removeWith(t, run, v, "alice/done")
			if res.OK || res.Reason != "changed" || res.BackupDir != "" || len(res.Files) != 0 || res.Detail == "" {
				t.Fatalf("res = %+v, want changed", res)
			}
			if !exists(filepath.Join(f.wt, ".env")) || !exists(filepath.Join(f.wt, "notes", "me.txt")) {
				t.Fatal("the folder was touched")
			}
			if exists(f.st.BackupDir(v.Repo, filepath.Base(f.wt), fixedNow())) {
				t.Fatal("the backup of a refused removal is left")
			}
			if len(f.opNames(t)) != 0 {
				t.Fatal("a refused removal must not be logged")
			}
		})
	}
}

// A commit made on a detached HEAD in the folder is reachable only from the
// folder's own HEAD reflog, which `worktree remove` deletes.
func TestRemoveFolderRefusesOrphanedCommits(t *testing.T) {
	f := newRemoveFixture(t)
	f.r.Git(f.wt, "checkout", "--detach")
	orphan := f.r.Commit(f.wt, "only here", map[string]string{"tmp.txt": "t\n"})
	f.r.Git(f.wt, "checkout", "alice/done")
	res := f.remove(t, f.view(t), "alice/done")
	if res.OK || res.Reason != "orphaned-commits" || len(res.Files) != 1 || res.Files[0] != orphan[:12] {
		t.Fatalf("res = %+v, want orphaned-commits %s", res, orphan[:12])
	}
	if !exists(f.wt) || len(f.opNames(t)) != 0 {
		t.Fatal("a refused removal touched something")
	}
	// Once a branch holds that commit, nothing is lost and the folder goes.
	f.r.Git(f.r.Dir, "branch", "keep", orphan)
	if res := f.remove(t, f.view(t), "alice/done"); !res.OK {
		t.Fatalf("res = %+v, want ok", res)
	}
}

// swapSync replaces the flush functions for one test.
func swapSync(t *testing.T, file func(*os.File) error, dir func(string) error) {
	t.Helper()
	oldFile, oldDir := syncFile, syncDir
	syncFile, syncDir = file, dir
	t.Cleanup(func() { syncFile, syncDir = oldFile, oldDir })
}

func TestRemoveFolderFlushesBackup(t *testing.T) {
	f := newRemoveFixture(t)
	f.write(t, ".env", "SECRET=1\n")
	f.write(t, "notes/me.txt", "n\n")
	files, dirs := 0, map[string]bool{}
	swapSync(t, func(*os.File) error { files++; return nil }, func(d string) error { dirs[d] = true; return nil })
	res := f.remove(t, f.view(t), "alice/done")
	if !res.OK {
		t.Fatalf("res = %+v", res)
	}
	if files != 2 {
		t.Fatalf("flushed %d files, want 2", files)
	}
	for _, d := range []string{res.BackupDir, filepath.Join(res.BackupDir, "notes"), filepath.Dir(res.BackupDir)} {
		if !dirs[d] {
			t.Fatalf("directory %s not flushed: %v", d, dirs)
		}
	}
}

func TestRemoveFolderFlushFailureKeepsFolder(t *testing.T) {
	f := newRemoveFixture(t)
	f.write(t, ".env", "SECRET=1\n")
	f.write(t, "node_modules/x.js", "x\n")
	v := f.view(t)
	swapSync(t, func(*os.File) error { return errors.New("disk full") }, syncDirectory)
	res := f.remove(t, v, "alice/done")
	if res.OK || res.Reason != "backup-failed" || !strings.Contains(res.Detail, "disk full") {
		t.Fatalf("res = %+v", res)
	}
	if len(res.DeletedBuild) != 0 {
		t.Fatalf("DeletedBuild = %v on a refusal", res.DeletedBuild)
	}
	if !exists(filepath.Join(f.wt, ".env")) || !exists(filepath.Join(f.wt, "node_modules", "x.js")) {
		t.Fatal("the folder was touched")
	}
	if exists(f.st.BackupDir(v.Repo, filepath.Base(f.wt), fixedNow())) {
		t.Fatal("a partial backup is left")
	}
}

// The second lock probe runs after the backup: a file opened meanwhile keeps
// the folder, and the finished backup is kept.
func TestRemoveFolderLockedAfterBackup(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("a folder can only be locked by an open file on Windows")
	}
	f := newRemoveFixture(t)
	f.write(t, ".env", "SECRET=1\n")
	f.write(t, "node_modules/x.js", "x\n")
	var held *os.File
	t.Cleanup(func() {
		if held != nil {
			held.Close()
		}
	})
	run := hookRunner{before: func(_ string, args []string) {
		if held == nil && len(args) > 1 && args[0] == "ls-files" && args[1] == "--others" {
			var err error
			if held, err = os.Open(filepath.Join(f.wt, "node_modules", "x.js")); err != nil {
				t.Error(err)
			}
		}
	}}
	res := f.removeWith(t, run, f.view(t), "alice/done")
	if res.OK || res.Reason != "locked" || res.BackupDir == "" || len(res.DeletedBuild) != 0 {
		t.Fatalf("res = %+v, want locked with the backup kept", res)
	}
	if readFile(t, filepath.Join(res.BackupDir, ".env")) != "SECRET=1\n" {
		t.Fatal("the backup was not kept")
	}
	if !exists(filepath.Join(f.wt, "node_modules", "x.js")) || exists(f.wt+lockProbeSuffix) || len(f.opNames(t)) != 0 {
		t.Fatal("the folder was changed or the refusal was logged")
	}
}

func TestRemoveFolderBackupIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not used on Windows")
	}
	f := newRemoveFixture(t)
	f.write(t, "notes/me.txt", "n\n")
	res := f.remove(t, f.view(t), "alice/done")
	if !res.OK {
		t.Fatalf("res = %+v", res)
	}
	for _, d := range []string{filepath.Join(f.st.Dir, "backups"), filepath.Dir(res.BackupDir), res.BackupDir, filepath.Join(res.BackupDir, "notes")} {
		info, err := os.Stat(d)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Fatalf("%s has mode %o, want 700", d, perm)
		}
	}
}
