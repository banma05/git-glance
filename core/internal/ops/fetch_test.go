package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/banma05/git-glance/core/internal/gitx"
)

// fetchFailRunner fails every fetch with err.
type fetchFailRunner struct {
	gitx.ExecRunner
	err   error
	calls int
}

func (f *fetchFailRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if args[0] == "fetch" {
		f.calls++
		return nil, f.err
	}
	return f.ExecRunner.Run(ctx, dir, args...)
}

// A fetch that ran into its timeout already waited 15 minutes: a second try
// would only double the wait.
func TestFetchDoesNotRetryAfterTimeout(t *testing.T) {
	r := &fetchFailRunner{err: fmt.Errorf("git fetch --all: %w", context.DeadlineExceeded)}
	res := Fetch(context.Background(), r, t.TempDir(), fixedNow)
	if res.OK || res.Attempts != 1 || r.calls != 1 || res.Error == "" {
		t.Fatalf("res = %+v after %d calls, want one attempt", res, r.calls)
	}
	r = &fetchFailRunner{err: &gitx.GitError{Args: []string{"fetch"}, ExitCode: 1, Stderr: "fatal: could not read\n"}}
	res = Fetch(context.Background(), r, t.TempDir(), fixedNow)
	if res.OK || res.Attempts != 2 || r.calls != 2 {
		t.Fatalf("res = %+v after %d calls, want a retry for other failures", res, r.calls)
	}
}

// git puts the path of a stale lock file in the first line of its message and
// advice after it: the detail keeps both.
func TestFailDetailKeepsLockPath(t *testing.T) {
	lock := &gitx.GitError{Args: []string{"merge"}, ExitCode: 128, Stderr: "fatal: Unable to create 'D:/r/.git/index.lock': File exists.\n\n" +
		"Another git process seems to be running in this repository, e.g.\n" +
		"an editor opened by 'git commit'. Please make sure all processes\n" +
		"are terminated then try again. If it still fails, a git process\n" +
		"may have crashed in this repository earlier:\nremove the file manually to continue.\n"}
	if d := failDetail(lock); !strings.Contains(d, "index.lock") || !strings.Contains(d, "remove the file manually") {
		t.Fatalf("detail = %q", d)
	}
	plain := &gitx.GitError{Args: []string{"merge"}, ExitCode: 1, Stderr: "first\nsecond\n\nthird\n"}
	if d := failDetail(plain); d != "second; third" {
		t.Fatalf("detail = %q, want the last two lines", d)
	}
	if d := failDetail(errors.New("boom")); d != "boom" {
		t.Fatalf("detail = %q", d)
	}
}
