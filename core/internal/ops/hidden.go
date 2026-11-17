package ops

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/banma05/git-glance/core/internal/gitx"
)

// hiddenChanges lists tracked files of the folder that git status cannot
// show as modified: marked skip-worktree or assume-unchanged, and present on
// disk. A marked file that is absent is a sparse checkout, not a change.
func hiddenChanges(ctx context.Context, r gitx.Runner, dir string) ([]string, error) {
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
		if _, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(path))); err == nil {
			files = append(files, path)
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
