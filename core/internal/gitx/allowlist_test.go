package gitx

import (
	"errors"
	"testing"
)

func TestAllowed(t *testing.T) {
	ok := [][]string{
		{"rev-parse", "--show-toplevel"},
		{"for-each-ref", "refs/heads"},
		{"fetch", "--all", "--prune", "--no-prune-tags"},
		{"fetch", ".", "refs/remotes/origin/x:refs/heads/x"},
		{"merge", "--ff-only", "--no-overwrite-ignore", "--no-autostash", "origin/x"},
		{"switch", "--no-guess", "--no-overwrite-ignore", "feature/a"},
		{"worktree", "list", "--porcelain"},
		{"worktree", "add", "--no-track", "-b", "x", "../p", "origin/main"},
		{"worktree", "add", "../p", "feature/x"},
		{"worktree", "remove", "../p"},
		{"symbolic-ref", "-q", "--short", "refs/remotes/origin/HEAD"},
		{"log", "--oneline", "-n", "3"},
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
		{"diff", "--output=x.patch", "a", "b"},
		{"log", "--output", "x"},
		{"show", "--output=x", "HEAD"},
		{"switch", "feature/a"},
		{"switch", "--no-guess", "feature/a"},
		{"switch", "--no-overwrite-ignore", "--no-guess", "a"},
		{"switch", "--no-guess", "--no-overwrite-ignore", "-x"},
		{"switch", "--no-guess", "--no-overwrite-ignore", "a", "b"},
		{"switch", "--no-guess", "-x"},
		{"switch", "--no-guess", "a", "b"},
		{"diff", "--outp=x", "a"},
		{"diff", "--outpu", "x"},
		{"worktree", "remove", "--forc", "../w"},
		{"worktree", "remove", "-ff", "../w"},
		{"worktree", "add", "-Bside", "../w3", "main"},
		{"worktree", "add", "--forc", "../w3", "main"},
		{"worktree", "add", "-ff", "../w3", "main"},
		{"worktree", "add", "--no-track", "-b", "x", "../p"},
		{"worktree", "add", "--no-track", "-b", "-x", "../p", "main"},
		{"worktree", "remove", "-x"},
		{"worktree", "list"},
		// The existing-branch shape is exactly `worktree add <path> <branch>`.
		// A path alone would create a branch named after the folder.
		{"worktree", "add", "../p"},
		{"worktree", "add", "../p", "x", "y"},
		{"worktree", "add", "-f", "../p"},
		{"worktree", "add", "--force", "../p"},
		{"worktree", "add", "--detach", "../p"},
		{"worktree", "add", "../p", "--detach"},
		{"worktree", "add", "../p", "-b"},
		{"worktree", "add", "../p", "-"},
		{"worktree", "add", "-", "x"},
		{"worktree", "add", "--lock", "../p"},
		{"worktree", "add", "--orphan", "x"},
		{"worktree", "add", "-B", "x"},
		{"worktree", "add", "../p", "--guess-remote"},
		{"worktree", "add", "", "x"},
		{"worktree", "add", "../p", ""},
		{"merge", "--ff-only", "origin/x"},
		{"fetch", "--all", "--prune"},
		{"merge", "--ff-only", "--no-overwrite-ignore", "-x"},
		// merge.autoStash=true would stash and re-apply the user's changes.
		{"merge", "--ff-only", "--no-overwrite-ignore", "origin/x"},
		{"merge", "--ff-only", "--no-overwrite-ignore", "--autostash", "x"},
		{"merge", "--ff-only", "--no-autostash", "--no-overwrite-ignore", "x"},
		{"merge", "--ff-only", "--no-overwrite-ignore", "--no-autostash", "-x"},
		{"merge", "--ff-only", "--no-overwrite-ignore", "--no-autostash", "x", "y"},
		{"merge", "--ff-only", "--no-overwrite-ignore", "--no-autostash"},
		{"merge", "--no-autostash", "--ff-only", "--no-overwrite-ignore", "x"},
		{"merge", "--ff-only", "--no-overwrite-ignore", "--squash", "x"},
		{"merge", "--ff-only", "--no-ff", "-m", "m", "side"},
		{"merge", "--ff-only", "--ff", "x"},
		{"merge", "--ff-only", "--squash", "x"},
		{"merge", "--ff-only", "-x"},
		{"fetch", ".", "--upload-pack=touch PWNED; git-upload-pack"},
		{"fetch", "--all", "--upload-pack=touch PWNED; git-upload-pack"},
		{"fetch", "--all", "--prune", "--prune-tags"},
		{"fetch", "--all", "-fp"},
		{"fetch", "--all", "--forc"},
		{"fetch", "--all"},
		{"fetch", ".", "--stdin"},
		{"fetch", "--all", "--stdin"},
		{"fetch", ".", "refs/remotes/origin/x:refs/heads/x:y"},
		{"fetch", ".", "refs/remotes/origin/x:"},
		{"fetch", ".", ":refs/heads/x"},
		{"fetch", ".", "refs/heads/x:refs/heads/y"},
		{"fetch", ".", "refs/remotes/origin/x:refs/remotes/origin/y"},
		{"fetch", ".", "-refs/remotes/origin/x:refs/heads/x"},
		{"fetch", ".", "refs/remotes/origin/*:refs/heads/*"},
		{"fetch", ".", "refs/remotes/origin/*:refs/heads/feature/*"},
		{"fetch", ".", "refs/remotes/origin/x:refs/heads/*"},
		{"symbolic-ref", "-q", "--short", "-x"},
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

func TestAllowedInput(t *testing.T) {
	if err := allowedInput([]string{"patch-id", "--stable"}); err != nil {
		t.Errorf("patch-id --stable rejected: %v", err)
	}
	bad := [][]string{
		{"fetch", ".", "--stdin"},
		{"fetch", "--all", "--stdin"},
		{"patch-id"},
		{"patch-id", "--stable", "x"},
		{"log"},
		{"push"},
		nil,
	}
	for _, a := range bad {
		if err := allowedInput(a); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("allowedInput(%q) = %v, want ErrNotAllowed", a, err)
		}
	}
}
