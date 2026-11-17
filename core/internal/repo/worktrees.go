package repo

import (
	"context"
	"os"
	"strings"

	"github.com/banma05/git-glance/core/internal/gitx"
)

func readWorktrees(ctx context.Context, r gitx.Runner, dir string) ([]Worktree, error) {
	out, err := r.Run(ctx, dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	list := parseWorktrees(string(out))
	for i := range list {
		w := &list[i]
		if _, err := os.Stat(w.Path); err != nil {
			w.Missing = true
			continue
		}
		if w.Dirty, err = readDirty(ctx, r, w.Path); err != nil {
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
		}
	}
	return list
}
