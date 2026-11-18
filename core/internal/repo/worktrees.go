package repo

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/banma05/git-glance/core/internal/gitx"
)

func readWorktrees(ctx context.Context, r gitx.Runner, dir string) ([]Worktree, error) {
	list, err := listWorktrees(ctx, r, dir)
	if err != nil {
		return nil, err
	}
	for i := range list {
		w := &list[i]
		if _, err := os.Stat(w.Path); err != nil {
			w.Missing = true
			continue
		}
		dirty, err := readDirty(ctx, r, w.Path)
		var ge *gitx.GitError
		switch {
		case err == nil:
			w.Dirty = dirty
		case errors.As(err, &ge) && ctx.Err() == nil:
			// git cannot read this folder (a broken .git file, no access):
			// mark it so nothing touches it, and show the rest.
			w.Missing, w.Broken = true, true
		default:
			return nil, err
		}
	}
	return list, nil
}

func parseWorktrees(out string) []Worktree {
	var list []Worktree
	var cur *Worktree
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "worktree "):
			p := strings.ReplaceAll(strings.TrimPrefix(line, "worktree "), "\\", "/")
			list = append(list, Worktree{Path: p, Main: len(list) == 0, Dirty: DirtyCounts{Files: []string{}}})
			cur = &list[len(list)-1]
		case cur == nil:
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "detached":
			cur.Detached = true
		case line == "locked" || strings.HasPrefix(line, "locked "):
			cur.Locked = true
		case line == "bare":
			cur.Bare = true
		}
	}
	return list
}
