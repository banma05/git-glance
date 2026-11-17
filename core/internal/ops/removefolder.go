package ops

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/model"
	"github.com/banma05/git-glance/core/internal/settings"
)

// BuildDirs are folder names whose ignored contents are build output: they
// are deleted with the folder and never backed up.
var BuildDirs = []string{"node_modules", "target", "build", "dist", "out", ".gradle", "__pycache__", ".pytest_cache", ".mypy_cache", ".tox", ".next", ".nuxt", "coverage", ".venv", "venv"}

const lockProbeSuffix = ".gitglance-check"

// RemoveFolderResult is the outcome of RemoveFolder. When OK is false nothing
// was deleted. Reason is the branch's RemoveBlock value, or changed (the
// folder is no longer on the branch), locked (the folder cannot be renamed,
// something is using it), backup-failed (the partial backup was removed) or
// git-failed (Detail is git's last error line; a finished backup is kept).
// Detail on success says the folder was not fully deleted, when it was not.
// Files lists the ignored paths that were copied to BackupDir, DeletedBuild
// the build output that was not, and SkippedLinks the links (symlinks,
// junctions) that were neither followed nor copied. LogError is empty unless
// writing the operation log failed.
type RemoveFolderResult struct {
	OK           bool     `json:"ok"`
	Reason       string   `json:"reason"`
	Files        []string `json:"files"`
	BackupDir    string   `json:"backupDir"`
	BackupBytes  int64    `json:"backupBytes"`
	DeletedBuild []string `json:"deletedBuild"`
	SkippedLinks []string `json:"skippedLinks"`
	Detail       string   `json:"detail"`
	LogError     string   `json:"logError"`
}

func removeRefused(reason string) RemoveFolderResult {
	return RemoveFolderResult{Reason: reason, Files: []string{}, DeletedBuild: []string{}, SkippedLinks: []string{}}
}

// RemoveFolder deletes the folder of branch from disk. The branch itself is
// never deleted. Ignored files that are not build output are copied to a
// backup directory first; the folder is deleted only when the copy is
// complete. v must be a view built just before the call.
func RemoveFolder(ctx context.Context, r gitx.Runner, st settings.Store, v model.View, branch string, now func() time.Time) (RemoveFolderResult, error) {
	bv, ok := v.Branches[branch]
	if !ok {
		return removeRefused(""), fmt.Errorf("%s is not a branch of this view", branch)
	}
	if !bv.CanRemoveFolder {
		return removeRefused(bv.RemoveBlock), nil
	}
	path := bv.Folder
	head, err := headRef(ctx, r, path)
	if err != nil {
		return removeRefused(""), err
	}
	if head != "refs/heads/"+branch {
		return removeRefused("changed"), nil
	}
	if reason, err := probeLock(path); err != nil || reason != "" {
		return removeRefused(reason), err
	}
	plan, err := planBackup(ctx, r, path)
	if err != nil {
		return removeRefused(""), err
	}
	res := removeRefused("")
	res.DeletedBuild = plan.build
	backupDir := st.BackupDir(v.Repo, filepath.Base(path), now())
	copied, err := copyIgnored(path, backupDir, plan.backup)
	res.SkippedLinks = copied.skipped
	if err != nil {
		res.Reason = "backup-failed"
		res.Detail = err.Error()
		if copied.created {
			_ = os.RemoveAll(backupDir)
		}
		return res, nil
	}
	res.Files, res.BackupBytes = copied.files, copied.bytes
	if copied.created {
		res.BackupDir = backupDir
	}
	cmd := []string{"worktree", "remove", path}
	entry := settings.OpEntry{
		Time: now().Unix(), Repo: v.Repo, Op: "remove-folder", Branch: branch, Folder: path,
		Command: append([]string{"git"}, cmd...), Before: head, Detail: res.BackupDir,
	}
	if _, err := r.Run(ctx, v.Repo, cmd...); err != nil {
		res.Reason, res.Detail = "git-failed", lastLine(err)
		entry.Result = "git-failed: " + res.Detail
		if err := st.AppendOp(entry); err != nil {
			res.LogError = err.Error()
		}
		return res, nil
	}
	res.OK = true
	res.Detail = sweepLinks(path)
	entry.Result = "ok"
	if err := st.AppendOp(entry); err != nil {
		res.LogError = err.Error()
	}
	return res, nil
}

// probeLock renames the folder away and back: an open file or a process
// working inside it makes the rename fail on Windows. It returns reason
// "locked" when the rename fails and an error when the probe name is taken or
// the folder cannot be put back.
func probeLock(path string) (string, error) {
	probe := path + lockProbeSuffix
	if _, err := os.Lstat(probe); err == nil {
		return "", fmt.Errorf("%s already exists; move it away and try again", probe)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err := os.Rename(path, probe); err != nil {
		return "locked", nil
	}
	var back error
	for i := 0; i < 5; i++ {
		if back = os.Rename(probe, path); back == nil {
			return "", nil
		}
		time.Sleep(400 * time.Millisecond)
	}
	return "", fmt.Errorf("could not rename %s back to %s: %w", probe, path, back)
}

type backupPlan struct {
	backup []string // ignored paths to copy, as git lists them
	build  []string // ignored build output, deleted with the folder
}

func isBuildPath(p string) bool {
	for _, seg := range strings.Split(strings.TrimSuffix(p, "/"), "/") {
		for _, name := range BuildDirs {
			if seg == name {
				return true
			}
		}
	}
	return false
}

// planBackup lists the ignored paths of the folder and splits them. Build
// names are matched only against the listed paths: whatever lies inside a
// listed non-build directory is backed up in full.
func planBackup(ctx context.Context, r gitx.Runner, path string) (backupPlan, error) {
	out, err := r.Run(ctx, path, "ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z")
	if err != nil {
		return backupPlan{}, err
	}
	plan := backupPlan{backup: []string{}, build: []string{}}
	for _, p := range strings.Split(string(out), "\x00") {
		switch {
		case p == "":
		case isBuildPath(p):
			plan.build = append(plan.build, p)
		default:
			plan.backup = append(plan.backup, p)
		}
	}
	return plan, nil
}

type copyResult struct {
	files   []string
	skipped []string
	bytes   int64
	created bool // this call started creating the backup directory
}

// copyIgnored copies the listed paths from folder to backupDir, keeping
// relative paths. It never follows links: a symlink, junction or any entry
// that is neither a regular file nor a plain directory is skipped and listed.
// The backup directory is created only when there is something to copy, and it
// must not exist yet.
func copyIgnored(folder, backupDir string, paths []string) (copyResult, error) {
	res := copyResult{files: []string{}, skipped: []string{}}
	for _, p := range paths {
		if err := copyTree(folder, backupDir, strings.TrimSuffix(p, "/"), &res); err != nil {
			return res, err
		}
	}
	return res, nil
}

func (c *copyResult) prepare(backupDir string) error {
	if c.created {
		return nil
	}
	if _, err := os.Lstat(backupDir); err == nil {
		return fmt.Errorf("backup directory %s already exists", backupDir)
	}
	c.created = true // a partial MkdirAll is ours to clean up
	return os.MkdirAll(backupDir, 0o755)
}

func copyTree(folder, backupDir, rel string, res *copyResult) error {
	root := filepath.Join(folder, filepath.FromSlash(rel))
	return filepath.WalkDir(root, func(src string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		sub, err := filepath.Rel(folder, src)
		if err != nil {
			return err
		}
		sub = filepath.ToSlash(sub)
		dst := filepath.Join(backupDir, filepath.FromSlash(sub))
		switch {
		case d.IsDir():
			if err := res.prepare(backupDir); err != nil {
				return err
			}
			return os.MkdirAll(dst, 0o755)
		case d.Type().IsRegular():
			if err := res.prepare(backupDir); err != nil {
				return err
			}
			n, err := copyFile(src, dst)
			if err != nil {
				return err
			}
			res.files, res.bytes = append(res.files, sub), res.bytes+n
		default:
			res.skipped = append(res.skipped, sub)
		}
		return nil
	})
}

func copyFile(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return 0, err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm()|0o600)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n != info.Size() {
		err = fmt.Errorf("copied %d of %d bytes of %s", n, info.Size(), src)
	}
	return n, err
}

// sweepLinks finishes what git leaves behind: git deletes the folder's files
// but can stop at a junction or symlink. Only the links themselves and the
// directories that become empty are removed; os.Remove on a link never reaches
// its target, and regular files are never touched. It returns a note when the
// folder is still there.
func sweepLinks(path string) string {
	var dirs []string
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
		case d.IsDir():
			dirs = append(dirs, p)
		case !d.Type().IsRegular():
			_ = os.Remove(p)
		}
		return nil
	})
	for i := len(dirs) - 1; i >= 0; i-- {
		_ = os.Remove(dirs[i])
	}
	if _, err := os.Lstat(path); err == nil {
		return "the folder still exists: " + path
	}
	return ""
}
