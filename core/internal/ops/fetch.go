// Package ops holds the operations that change the repository: fetching and
// aligning local branches with their remote counterparts.
package ops

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/banma05/git-glance/core/internal/gitx"
)

// FetchResult describes one fetch run (up to two attempts).
type FetchResult struct {
	OK       bool   `json:"ok"`
	Attempts int    `json:"attempts"`
	Error    string `json:"error"` // last non-empty stderr line of the last attempt
	At       int64  `json:"at"`    // unix seconds
}

// Fetch runs `fetch --all --prune --no-prune-tags` and retries once on
// failure, but not after a timeout: that attempt already waited the runner's
// whole fetch timeout.
func Fetch(ctx context.Context, r gitx.Runner, repoRoot string, now func() time.Time) FetchResult {
	res := FetchResult{}
	for res.Attempts < 2 {
		res.Attempts++
		_, err := r.Run(ctx, repoRoot, "fetch", "--all", "--prune", "--no-prune-tags")
		if err == nil {
			res.OK, res.Error = true, ""
			break
		}
		res.Error = lastLine(err)
		if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) {
			break
		}
	}
	res.At = now().Unix()
	return res
}

// lastLine returns the last non-empty stderr line of a git error, or the error
// text for any other error.
func lastLine(err error) string {
	text := err.Error()
	var ge *gitx.GitError
	if errors.As(err, &ge) {
		text = ge.Stderr
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r", ""), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}

// failDetail is what a failed git command tells the user: the last two
// non-empty stderr lines or, when an earlier line names a lock file (git
// prints its path first and advice after it), that line and the last one.
// Any other error gives its text.
func failDetail(err error) string {
	var ge *gitx.GitError
	if !errors.As(err, &ge) {
		return lastLine(err)
	}
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(ge.Stderr, "\r", ""), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	switch len(lines) {
	case 0:
		return ""
	case 1:
		return lines[0]
	}
	last := lines[len(lines)-1]
	for _, l := range lines[:len(lines)-1] {
		if strings.Contains(l, ".lock") {
			return l + "; " + last
		}
	}
	return lines[len(lines)-2] + "; " + last
}
