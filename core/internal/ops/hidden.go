package ops

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/banma05/git-glance/core/internal/gitx"
)

// hiddenChanges lists tracked files of the folder that git status cannot
// show as modified: marked skip-worktree or assume-unchanged, and present on
// disk. A marked file that is absent is a sparse checkout, not a change.
func hiddenChanges(ctx context.Context, r gitx.Runner, dir string) ([]string, error) {
	return hiddenChangesWith(ctx, r, dir, os.Lstat)
}

func hiddenChangesWith(ctx context.Context, r gitx.Runner, dir string, lstat func(string) (fs.FileInfo, error)) ([]string, error) {
	out, err := r.Run(ctx, dir, "ls-files", "-v", "-z")
	if err != nil {
		return nil, err
	}
	files := []string{}
	for _, entry := range strings.Split(string(out), "\x00") {
		if len(entry) < 3 || entry[1] != ' ' {
			continue
		}
		tag, path := entry[0], entry[2:]
		if tag != 'S' && !(tag >= 'a' && tag <= 'z') {
			continue
		}
		_, err := lstat(filepath.Join(dir, filepath.FromSlash(path)))
		switch {
		case err == nil:
			files = append(files, path)
		case errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR):
			// Absent: a sparse checkout entry.
		default:
			// Unknown is not absent: refuse rather than miss a change.
			return nil, err
		}
	}
	return files, nil
}

func capFiles(files []string) []string {
	if len(files) > maxSkipFiles {
		return files[:maxSkipFiles]
	}
	return files
}
