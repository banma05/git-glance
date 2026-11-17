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

// Fetch runs `fetch --all --prune` and retries once on failure. The timeout of
// each attempt is the one of the runner.
func Fetch(ctx context.Context, r gitx.Runner, repoRoot string, now func() time.Time) FetchResult {
	res := FetchResult{}
	for res.Attempts < 2 {
		res.Attempts++
		_, err := r.Run(ctx, repoRoot, "fetch", "--all", "--prune")
		if err == nil {
			res.OK, res.Error = true, ""
			break
		}
		res.Error = lastLine(err)
		if ctx.Err() != nil {
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
