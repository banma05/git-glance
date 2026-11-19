package model

import (
	"context"
	"strings"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/repo"
)

const createdFrom = "branch: Created from "

// creationRecord reads git's own note of where a local branch was created: the
// oldest reflog entry, when it says "branch: Created from <start>". Anything
// else (no reflog, an expired creation entry, a failed or cancelled read)
// means no record and returns "".
func creationRecord(ctx context.Context, r gitx.Runner, dir, branch string) string {
	out, err := r.Run(ctx, dir, "log", "--walk-reflogs", "--format=%gs", "refs/heads/"+branch, "--")
	if err != nil {
		return ""
	}
	lines := strings.Split(string(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		start, ok := strings.CutPrefix(line, createdFrom)
		if !ok {
			return ""
		}
		return strings.TrimSpace(start)
	}
	return ""
}

// trunkNamed returns the tied trunk that start names: the trunk's name, its
// refs/heads/ form, or either spelling on a remote of the snapshot.
func trunkNamed(start string, tied, remotes []string) string {
	if start == "" {
		return ""
	}
	for _, t := range tied {
		if start == t || start == "refs/heads/"+t {
			return t
		}
		for _, rm := range remotes {
			if start == rm+"/"+t || start == "refs/remotes/"+rm+"/"+t {
				return t
			}
		}
	}
	return ""
}

// remoteNames lists the remotes that have a tracking ref in the snapshot.
func remoteNames(snap repo.Snapshot) []string {
	seen := map[string]bool{}
	var out []string
	for _, ref := range snap.Remote {
		if i := strings.Index(ref.Name, "/"); i > 0 && !seen[ref.Name[:i]] {
			seen[ref.Name[:i]] = true
			out = append(out, ref.Name[:i])
		}
	}
	return out
}
