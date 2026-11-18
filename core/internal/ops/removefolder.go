package ops

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/model"
	"github.com/banma05/git-glance/core/internal/settings"
)

// BuildDirs are folder names whose ignored contents are build output: they
// are deleted with the folder and never backed up. Only an ignored directory
// whose own name is in the list counts; files and other directories are always
// backed up, wherever they lie.
var BuildDirs = []string{"node_modules", "target", "build", "dist", "out", ".gradle", "__pycache__", ".pytest_cache", ".mypy_cache", ".tox", ".next", ".nuxt", "coverage", ".venv", "venv"}

const lockProbeSuffix = ".gitglance-check"

// noSignature goes on every log: log.showSignature=true in the user's config
// would otherwise print signature lines into parsed output.
const noSignature = "--no-show-signature"

// RemoveFolderResult is the outcome of RemoveFolder. When OK is false and
// Reason is a refusal (the branch's RemoveBlock value, changed, hidden-changes,
// orphaned-commits, nested-repo, locked, backup-failed) the folder was not
// deleted. Reasons: changed (the folder is not the one the caller saw, is no
// longer on the branch or at the same commit, or its ignored files changed
// during the backup; Detail says what, and this call's backup was removed),
// hidden-changes (Files lists tracked files marked skip-worktree or
// assume-unchanged that exist on disk), orphaned-commits (Files lists short
// hashes of commits only the folder's own HEAD history reaches), nested-repo
// (Files lists .git entries inside build output), locked (the folder cannot
// be renamed, something is using it; a finished backup is kept),
// backup-failed (the partial backup was removed) and git-failed (Detail is
// git's error and says whether the folder still exists, as git may have
// removed part of it; a finished backup is kept). Files lists the ignored
// paths that were copied to BackupDir, DeletedBuild the build output that was
// deleted (set only on success), and SkippedLinks the links (symlinks,
// junctions) that were neither followed nor copied; junctions are removed
// before git runs. On success Detail says so when the folder was not fully
// deleted. LogError is empty unless writing the operation log failed.
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

func refusedWith(reason, detail string, files []string) RemoveFolderResult {
	res := removeRefused(reason)
	res.Detail = detail
	if files != nil {
		res.Files = capFiles(files)
	}
	return res
}

// RemoveFolder deletes the folder of branch from disk. The branch itself is
// never deleted. folder and head are the folder and the HEAD hash the caller
// showed; anything else is refused as changed. Ignored files that are not
// build output are copied to a backup directory first; the folder is deleted
// only when the copy is complete and nothing changed meanwhile. v must be a
// view built just before the call.
func RemoveFolder(ctx context.Context, r gitx.Runner, st settings.Store, v model.View, branch, folder, head string, now func() time.Time) (RemoveFolderResult, error) {
	bv, ok := v.Branches[branch]
	if !ok {
		return removeRefused(""), fmt.Errorf("%s is not a branch of this view", branch)
	}
	if !bv.CanRemoveFolder {
		return removeRefused(bv.RemoveBlock), nil
	}
	path := bv.Folder
	if !samePath(path, folder) {
		return refusedWith("changed", "the folder of "+branch+" is "+path+", not "+folder, nil), nil
	}
	if res, done, err := checkFolder(ctx, r, v.Repo, path, branch, head); done || err != nil {
		return res, err
	}
	if reason, err := probeLock(path); err != nil || reason != "" {
		return removeRefused(reason), err
	}
	plan, err := planBackup(ctx, r, path)
	if err != nil {
		return removeRefused(""), err
	}
	if len(plan.nested) > 0 {
		return refusedWith("nested-repo", "", plan.nested), nil
	}
	backupDir := st.BackupDir(v.Repo, filepath.Base(path), now())
	res, copied := backUp(path, backupDir, plan)
	if res.Reason != "" {
		return res, nil
	}
	// The copy can take long: make sure nothing locked the folder meanwhile.
	if reason, err := probeLock(path); err != nil || reason != "" {
		res.Reason = reason
		return res, err
	}
	why, err := changedDuringBackup(ctx, r, v.Repo, path, branch, head, plan, copied)
	if err != nil || why != "" {
		// Nothing was removed, so this call's backup is not needed.
		res = abandonBackup(res, copied.created, backupDir)
		res.Reason, res.Detail = "changed", joinDetail(why, res.Detail)
		return res, err
	}
	dropped, err := dropJunctions(path)
	res.SkippedLinks = appendNew(res.SkippedLinks, dropped)
	if err != nil {
		// Only links were removed, so the folder still holds every file and
		// this call's backup is not needed.
		return abandonBackup(res, copied.created, backupDir), err
	}
	return removeWorktree(ctx, r, st, v.Repo, branch, path, head, plan, res, now), nil
}

// checkFolder refuses when the folder is no longer on branch at head, holds
// hidden changes, or holds commits that only its own HEAD history reaches.
// done is true when res is a refusal.
func checkFolder(ctx context.Context, r gitx.Runner, repoRoot, path, branch, head string) (RemoveFolderResult, bool, error) {
	if why, err := folderMoved(ctx, r, path, branch, head); err != nil || why != "" {
		return refusedWith("changed", why, nil), true, err
	}
	hidden, err := hiddenChanges(ctx, r, path)
	if err != nil || len(hidden) > 0 {
		return refusedWith("hidden-changes", "", hidden), true, err
	}
	orphans, err := orphanedCommits(ctx, r, repoRoot, path)
	if err != nil || len(orphans) > 0 {
		return refusedWith("orphaned-commits", "", orphans), true, err
	}
	return RemoveFolderResult{}, false, nil
}

// folderMoved explains why the folder is no longer on branch at head, or
// returns "".
func folderMoved(ctx context.Context, r gitx.Runner, path, branch, head string) (string, error) {
	ref, err := headRef(ctx, r, path)
	if err != nil {
		return "", err
	}
	if why := wrongHead(ref, "refs/heads/"+branch); why != "" {
		return why, nil
	}
	at, err := revParse(ctx, r, path, "HEAD")
	if err != nil {
		return "", err
	}
	if at != head {
		return "HEAD of the folder is now " + at + ", not " + head, nil
	}
	return "", nil
}

// backUp copies the ignored files of the plan. On failure the returned result
// has Reason backup-failed and the partial backup is removed.
func backUp(path, backupDir string, plan backupPlan) (RemoveFolderResult, copyResult) {
	res := removeRefused("")
	copied, err := copyIgnored(path, backupDir, plan.backup)
	res.SkippedLinks = copied.skipped
	if err != nil {
		res.Reason = "backup-failed"
		res.Detail = err.Error()
		if copied.created {
			if rmErr := os.RemoveAll(backupDir); rmErr != nil {
				res.Detail += "; could not remove the partial backup " + backupDir + ": " + rmErr.Error()
			}
		}
		return res, copied
	}
	res.Files, res.BackupBytes = copied.files, copied.bytes
	if copied.created {
		res.BackupDir = backupDir
	}
	return res, copied
}

// changedDuringBackup re-reads, after the copy, everything the decision was
// based on: the list of ignored paths, every copied file (size and time), the
// branch and commit of the folder, its hidden changes and the commits only its
// HEAD history reaches. It explains the first difference, or returns "".
func changedDuringBackup(ctx context.Context, r gitx.Runner, repoRoot, path, branch, head string, plan backupPlan, copied copyResult) (string, error) {
	listed, err := listIgnored(ctx, r, path)
	if err != nil {
		return "", err
	}
	if !slices.Equal(listed, plan.listed) {
		return "the ignored files of the folder changed during the backup", nil
	}
	stamps, err := scanIgnored(path, plan.backup)
	if err != nil {
		return "", err
	}
	if rel := firstDifference(copied.stamps, stamps); rel != "" {
		return rel + " changed during the backup", nil
	}
	if why, err := folderMoved(ctx, r, path, branch, head); err != nil || why != "" {
		return why, err
	}
	hidden, err := hiddenChanges(ctx, r, path)
	if err != nil {
		return "", err
	}
	if len(hidden) > 0 {
		return "tracked files were marked skip-worktree or assume-unchanged during the backup", nil
	}
	orphans, err := orphanedCommits(ctx, r, repoRoot, path)
	if err != nil {
		return "", err
	}
	if len(orphans) > 0 {
		return "commits only the folder's HEAD history reaches appeared during the backup: " + strings.Join(orphans, ", "), nil
	}
	return "", nil
}

// removeWorktree runs `worktree remove` and logs it, whatever the outcome.
func removeWorktree(ctx context.Context, r gitx.Runner, st settings.Store, repoRoot, branch, path, head string, plan backupPlan, res RemoveFolderResult, now func() time.Time) RemoveFolderResult {
	cmd := []string{"worktree", "remove", path}
	entry := settings.OpEntry{
		Time: now().Unix(), Repo: repoRoot, Op: "remove-folder", Branch: branch, Folder: path,
		Command: append([]string{"git"}, cmd...), Before: head, Detail: res.BackupDir,
	}
	if _, err := r.Run(ctx, repoRoot, cmd...); err != nil {
		res.Reason, res.Detail = "git-failed", failDetail(err)+" ("+folderState(path)+")"
		res.LogError = logRan(st, entry, "git-failed: "+res.Detail, "")
		return res
	}
	res.OK = true
	res.DeletedBuild = plan.build
	res.Detail = sweepLinks(path)
	res.LogError = logRan(st, entry, "ok", "")
	return res
}

// maxRevArgs caps the revisions passed to one git command.
const maxRevArgs = 200

// orphanedCommits returns short hashes (at most maxSkipFiles) of commits that
// only the folder's own HEAD reflog reaches, for example work committed on a
// detached HEAD there. `worktree remove` deletes that reflog with the folder,
// so such commits would be lost. The check runs in the main folder, whose
// --single-worktree view leaves out every other folder's HEAD and reflog.
func orphanedCommits(ctx context.Context, r gitx.Runner, repoRoot, folder string) ([]string, error) {
	out, err := r.Run(ctx, folder, "log", "-g", noSignature, "--format=%H", "HEAD", "--")
	if err != nil {
		return nil, err
	}
	var hashes []string
	seen := map[string]bool{}
	for _, h := range strings.Fields(string(out)) {
		if isHash(h) && !seen[h] {
			seen[h] = true
			hashes = append(hashes, h)
		}
	}
	orphans := []string{}
	found := map[string]bool{}
	for start := 0; start < len(hashes) && len(orphans) < maxSkipFiles; start += maxRevArgs {
		args := append([]string{"rev-list", "--max-count=" + strconv.Itoa(maxSkipFiles)}, hashes[start:min(start+maxRevArgs, len(hashes))]...)
		out, err := r.Run(ctx, repoRoot, append(args, "--not", "--single-worktree", "--all", "--reflog")...)
		if err != nil {
			return nil, err
		}
		for _, h := range strings.Fields(string(out)) {
			if !found[h] && len(orphans) < maxSkipFiles {
				found[h] = true
				orphans = append(orphans, h[:min(len(h), 12)])
			}
		}
	}
	return orphans, nil
}

// isHash reports whether s is a full SHA-1 or SHA-256 object name.
func isHash(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f') {
			return false
		}
	}
	return true
}

func folderState(path string) string {
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		return "the folder is gone"
	}
	return "the folder still exists"
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

// abandonBackup removes the backup this call created and clears it from res.
// It is used only when the folder was not touched, so the copy is not needed.
func abandonBackup(res RemoveFolderResult, created bool, backupDir string) RemoveFolderResult {
	if created {
		if err := os.RemoveAll(backupDir); err != nil {
			res.Detail = joinDetail(res.Detail, "could not remove the backup "+backupDir+": "+err.Error())
		}
	}
	res.BackupDir, res.BackupBytes, res.Files = "", 0, []string{}
	return res
}

// joinDetail joins the non-empty parts with "; ".
func joinDetail(parts ...string) string {
	kept := []string{}
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "; ")
}

// appendNew appends the entries of add that list does not hold yet.
func appendNew(list, add []string) []string {
	for _, a := range add {
		if !slices.Contains(list, a) {
			list = append(list, a)
		}
	}
	return list
}

// dropJunctions removes the links in the folder that are not plain symlinks:
// junctions and other reparse points. Older Git for Windows takes a junction
// for a directory and deletes the files of its target during `worktree
// remove`; os.Remove on such a link removes only the link. Plain symlinks are
// left to git, and nothing else is touched. It returns the removed links,
// relative to the folder.
func dropJunctions(folder string) ([]string, error) {
	var links []string
	err := filepath.WalkDir(folder, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink == 0 && isLink(p, d) {
			links = append(links, p)
		}
		return nil
	})
	if err != nil {
		return []string{}, err
	}
	removed := []string{}
	for _, p := range links {
		if err := os.Remove(p); err != nil {
			return removed, err
		}
		rel, err := filepath.Rel(folder, p)
		if err != nil {
			return removed, err
		}
		removed = append(removed, filepath.ToSlash(rel))
	}
	return removed, nil
}

type backupPlan struct {
	listed []string // every ignored path, as git lists them
	backup []string // ignored paths to copy
	build  []string // ignored build output, deleted with the folder
	nested []string // .git entries inside build output: nested repositories
}

// isBuildPath reports whether a listed ignored path is a build directory: a
// directory entry whose last segment is a BuildDirs name.
func isBuildPath(p string) bool {
	if !strings.HasSuffix(p, "/") {
		return false
	}
	name := p[strings.LastIndex(strings.TrimSuffix(p, "/"), "/")+1 : len(p)-1]
	for _, b := range BuildDirs {
		if name == b {
			return true
		}
	}
	return false
}

// dropNested removes every path that lies under another listed directory.
func dropNested(paths []string) []string {
	out := []string{}
	for _, p := range paths {
		if !nestedIn(p, paths) {
			out = append(out, p)
		}
	}
	return out
}

// planBackup lists the ignored paths of the folder and splits them. Whatever
// lies inside a listed non-build directory is backed up in full.
func planBackup(ctx context.Context, r gitx.Runner, path string) (backupPlan, error) {
	listed, err := listIgnored(ctx, r, path)
	if err != nil {
		return backupPlan{}, err
	}
	plan := backupPlan{listed: listed, backup: []string{}, build: []string{}, nested: []string{}}
	// A build-named directory is build output only when git lists it alone:
	// listed files beneath it are ignored by file patterns and must be kept.
	rest := []string{}
	for _, p := range listed {
		if isBuildPath(p) && !hasListedChild(p, listed) {
			if !nestedIn(p, listed) {
				plan.build = append(plan.build, p)
				repoAt, err := nestedRepo(path, p)
				if err != nil {
					return backupPlan{}, err
				}
				if repoAt != "" {
					plan.nested = append(plan.nested, repoAt)
				}
			}
			continue
		}
		rest = append(rest, p)
	}
	plan.backup = dropNested(rest)
	return plan, nil
}

// listIgnored returns the ignored paths of the folder as git lists them: an
// ignored directory as one entry ending in "/".
func listIgnored(ctx context.Context, r gitx.Runner, path string) ([]string, error) {
	out, err := r.Run(ctx, path, "ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z")
	if err != nil {
		return nil, err
	}
	listed := []string{}
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			listed = append(listed, p)
		}
	}
	return listed, nil
}

// nestedRepo returns the first .git entry (file or directory) found inside the
// listed directory dir of folder, relative to folder, or "". Such a directory
// is a repository of its own, which may hold commits found nowhere else. The
// walk never follows links.
func nestedRepo(folder, dir string) (string, error) {
	found := ""
	root := filepath.Join(folder, filepath.FromSlash(strings.TrimSuffix(dir, "/")))
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root || !strings.EqualFold(d.Name(), ".git") {
			return nil
		}
		rel, err := filepath.Rel(folder, p)
		if err != nil {
			return err
		}
		found = filepath.ToSlash(rel)
		return fs.SkipAll
	})
	return found, err
}

func hasListedChild(dir string, listed []string) bool {
	for _, q := range listed {
		if q != dir && strings.HasPrefix(q, dir) {
			return true
		}
	}
	return false
}

// nestedIn reports whether p lies under another listed directory.
func nestedIn(p string, listed []string) bool {
	for _, q := range listed {
		if q != p && strings.HasSuffix(q, "/") && strings.HasPrefix(p, q) {
			return true
		}
	}
	return false
}

// isLink reports whether an entry is a symlink or a junction: something whose
// target must not be followed. Other irregular entries are not links.
func isLink(path string, d fs.DirEntry) bool {
	if d.Type()&fs.ModeSymlink != 0 {
		return true
	}
	if d.IsDir() || d.Type().IsRegular() {
		return false
	}
	_, err := os.Readlink(path)
	return err == nil
}

// stamp is what a re-check compares: size and modification time of a file,
// or the mere presence of a link.
type stamp struct {
	size int64
	mod  time.Time
	link bool
}

func (s stamp) same(o stamp) bool { return s.size == o.size && s.mod.Equal(o.mod) && s.link == o.link }

func fileStamp(path string) (stamp, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return stamp{}, err
	}
	return stamp{size: info.Size(), mod: info.ModTime()}, nil
}

// firstDifference returns a path whose stamp differs between a and b, or "".
func firstDifference(a, b map[string]stamp) string {
	keys := make([]string, 0, len(a)+len(b))
	for k := range a {
		keys = append(keys, k)
	}
	for k := range b {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		x, inA := a[k]
		y, inB := b[k]
		if inA != inB || !x.same(y) {
			return k
		}
	}
	return ""
}

// walkIgnored visits every entry under the listed paths of folder, with its
// path relative to folder. Links are never followed.
func walkIgnored(folder string, paths []string, visit func(src, rel string, d fs.DirEntry) error) error {
	for _, p := range paths {
		root := filepath.Join(folder, filepath.FromSlash(strings.TrimSuffix(p, "/")))
		err := filepath.WalkDir(root, func(src string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(folder, src)
			if err != nil {
				return err
			}
			return visit(src, filepath.ToSlash(rel), d)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// scanIgnored stamps every file and link under the listed paths, as
// copyIgnored does while copying.
func scanIgnored(folder string, paths []string) (map[string]stamp, error) {
	stamps := map[string]stamp{}
	err := walkIgnored(folder, paths, func(src, rel string, d fs.DirEntry) error {
		switch {
		case d.IsDir():
		case d.Type().IsRegular():
			s, err := fileStamp(src)
			if err != nil {
				return err
			}
			stamps[rel] = s
		case isLink(src, d):
			stamps[rel] = stamp{link: true}
		default:
			return fmt.Errorf("%s is neither a file, a directory nor a link", rel)
		}
		return nil
	})
	return stamps, err
}

type copyResult struct {
	files   []string
	skipped []string
	bytes   int64
	stamps  map[string]stamp // every copied file and skipped link
	created bool             // this call created the backup directory
}

// copyIgnored copies the listed paths from folder to backupDir, keeping
// relative paths, and flushes the copy to disk. It never follows links: a
// symlink or junction is skipped and listed. Any other entry that is neither
// a regular file nor a directory fails the copy. The backup directory is
// created only when there is something to copy, and it must not exist yet.
func copyIgnored(folder, backupDir string, paths []string) (copyResult, error) {
	res := copyResult{files: []string{}, skipped: []string{}, stamps: map[string]stamp{}}
	err := walkIgnored(folder, paths, func(src, rel string, d fs.DirEntry) error {
		dst := filepath.Join(backupDir, filepath.FromSlash(rel))
		switch {
		case d.IsDir():
			if err := res.prepare(backupDir); err != nil {
				return err
			}
			return os.MkdirAll(dst, 0o700)
		case d.Type().IsRegular():
			if err := res.prepare(backupDir); err != nil {
				return err
			}
			s, err := fileStamp(src)
			if err != nil {
				return err
			}
			n, err := copyFile(src, dst)
			if err != nil {
				return err
			}
			res.files, res.bytes, res.stamps[rel] = append(res.files, rel), res.bytes+n, s
		case isLink(src, d):
			res.skipped, res.stamps[rel] = append(res.skipped, rel), stamp{link: true}
		default:
			return fmt.Errorf("%s is neither a file, a directory nor a link", rel)
		}
		return nil
	})
	if err == nil && res.created {
		err = syncTree(backupDir)
	}
	return res, err
}

// prepare creates the backup directory, which this call then owns. Mkdir
// fails when the directory exists, so a backup of another call is never
// adopted and never removed.
func (c *copyResult) prepare(backupDir string) error {
	if c.created {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(backupDir), 0o700); err != nil {
		return err
	}
	if err := os.Mkdir(backupDir, 0o700); err != nil {
		return err
	}
	c.created = true
	return nil
}

// syncFile and syncDir flush a copied file and a backup directory to disk;
// tests replace them.
var (
	syncFile = func(f *os.File) error { return f.Sync() }
	syncDir  = syncDirectory
)

// syncDirectory flushes a directory's entries where the system can do that.
func syncDirectory(dir string) error {
	if runtime.GOOS == "windows" {
		return nil // Windows cannot flush a directory handle
	}
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Sync(); err != nil && !errors.Is(err, syscall.EINVAL) && !errors.Is(err, errors.ErrUnsupported) {
		return err
	}
	return nil
}

// syncTree flushes every directory of the backup, deepest first, and the
// directory that holds it.
func syncTree(backupDir string) error {
	var dirs []string
	err := filepath.WalkDir(backupDir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			dirs = append(dirs, p)
		}
		return err
	})
	if err != nil {
		return err
	}
	// Walk order puts a directory before its children; the holder goes first
	// so that it is flushed last.
	dirs = append([]string{filepath.Dir(backupDir)}, dirs...)
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := syncDir(dirs[i]); err != nil {
			return err
		}
	}
	return nil
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
	if err == nil {
		err = syncFile(out)
	}
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
// its target, and regular files and other entries are never touched. It
// returns a note when the folder is still there.
func sweepLinks(path string) string {
	var dirs []string
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
		case d.IsDir():
			dirs = append(dirs, p)
		case isLink(p, d):
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
