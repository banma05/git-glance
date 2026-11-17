package gitx

import (
	"errors"
	"testing"
)

func TestAllowed(t *testing.T) {
	ok := [][]string{
		{"rev-parse", "--show-toplevel"},
		{"for-each-ref", "refs/heads"},
		{"fetch", "--all", "--prune"},
		{"fetch", ".", "refs/remotes/origin/x:refs/heads/x"},
		{"merge", "--ff-only", "origin/x"},
		{"switch", "feature/a"},
		{"worktree", "list", "--porcelain"},
		{"worktree", "add", "--no-track", "-b", "x", "../p", "origin/main"},
		{"worktree", "remove", "../p"},
		{"symbolic-ref", "-q", "--short", "refs/remotes/origin/HEAD"},
		{"patch-id", "--stable"},
	}
	for _, a := range ok {
		if err := Allowed(a); err != nil {
			t.Errorf("Allowed(%q) = %v, want nil", a, err)
		}
	}
	bad := [][]string{
		{"push", "origin", "main"},
		{"reset", "--hard"},
		{"branch", "-D", "x"},
		{"clean", "-fdx"},
		{"rebase", "main"},
		{"stash", "drop"},
		{"pull"},
		{"commit", "-m", "x"},
		{"checkout", "x"},
		{"cherry-pick", "abc"},
		{"tag", "v1"},
		{"update-ref", "refs/heads/x", "abc"},
		{"config", "user.email", "x"},
		{"remote", "set-url", "origin", "x"},
		{"merge", "main"},
		{"merge", "--no-ff", "x"},
		{"fetch", "origin", "+main:main"},
		{"fetch", "-f", "origin", "x:y"},
		{"fetch", "origin", "x:y"},
		{"fetch", "--force", ".", "x:y"},
		{"fetch", ".", "+x:y"},
		{"fetch", ".", "x:y", "extra"},
		{"fetch", "--all", "+x"},
		{"switch", "-C", "x"},
		{"switch", "--discard-changes", "x"},
		{"switch"},
		{"worktree", "remove", "--force", "p"},
		{"worktree", "remove", "-f", "p"},
		{"worktree", "add", "-B", "x", "p"},
		{"worktree", "add", "--force", "p"},
		{"worktree", "add", "-f", "p"},
		{"worktree", "prune"},
		{"worktree"},
		{"symbolic-ref", "HEAD", "refs/heads/x"},
		{"-c", "alias.st=push", "st"},
		{"-C", "x", "status"},
		{},
		nil,
		{""},
	}
	for _, a := range bad {
		if err := Allowed(a); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("Allowed(%q) = %v, want ErrNotAllowed", a, err)
		}
	}
}
