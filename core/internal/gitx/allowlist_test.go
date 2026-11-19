package gitx

import (
	"errors"
	"strings"
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

// stash store puts back an entry that a drop removed by mistake: exactly
// --message <one-line text> <full hash>. ls-tree is a read.
func TestAllowedStoreShape(t *testing.T) {
	sha1 := "0123456789abcdef0123456789abcdef01234567"
	ok := [][]string{
		{"stash", "store", "--message", "On main: mine", sha1},
		{"stash", "store", "--message", StoreMessage("On main: mine"), sha1 + "89abcdef0123456789abcdef"},
		{"ls-tree", "-r", "--name-only", "-z", sha1 + "^3"},
	}
	for _, a := range ok {
		if err := Allowed(a); err != nil {
			t.Errorf("Allowed(%q) = %v, want nil", a, err)
		}
	}
	bad := [][]string{
		{"stash", "store", sha1},
		{"stash", "store", "-m", "x", sha1},
		{"stash", "store", "--message=x", sha1},
		{"stash", "store", "-q", "--message", "x", sha1},
		{"stash", "store", "--message", "x"},
		{"stash", "store", "--message", "x", sha1, "y"},
		{"stash", "store", "--message", "x", "--", sha1},
		{"stash", "store", "--message", "", sha1},
		{"stash", "store", "--message", "-x", sha1},
		{"stash", "store", "--message", "a\nb", sha1},
		{"stash", "store", "--message", strings.Repeat("m", 1001), sha1},
		{"stash", "store", "--message", "x", sha1[:39]},
		{"stash", "store", "--message", "x", "0123456789ABCDEF0123456789abcdef01234567"},
		{"stash", "store", "--message", "x", "stash@{0}"},
		{"stash", "store", "--message", "x", "HEAD"},
		{"ls-tree", "-r", "--output=x", "HEAD"},
	}
	for _, a := range bad {
		if err := Allowed(a); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("Allowed(%q) = %v, want ErrNotAllowed", a, err)
		}
	}
}

func TestShelfAndStoreMessages(t *testing.T) {
	msg := ShelfMessage("feature/login", 1700000000)
	if b, at, ok := ParseShelfMessage(msg); !ok || b != "feature/login" || at != 1700000000 {
		t.Fatalf("ParseShelfMessage(%q) = %q %d %v", msg, b, at, ok)
	}
	for _, m := range []string{"", "my work", "git-glance shelf main", "git-glance shelf main x", "git-glance shelf -x 1", "git-glance shelf a b 1"} {
		if _, _, ok := ParseShelfMessage(m); ok {
			t.Errorf("ParseShelfMessage(%q) ok", m)
		}
	}
	for in, want := range map[string]string{
		"On main: mine": "On main: mine",
		"":              "stored again by git-glance",
		"-x":            "stored again by git-glance",
		"a\nb":          "stored again by git-glance",
	} {
		if got := StoreMessage(in); got != want {
			t.Errorf("StoreMessage(%q) = %q, want %q", in, got, want)
		}
	}
}

// The shapes that shelve, restore and drop uncommitted work, and create a
// local branch from a remote one, are exact; every near miss is refused.
func TestAllowedShelfAndTrackShapes(t *testing.T) {
	sha1 := "0123456789abcdef0123456789abcdef01234567"
	sha256 := sha1 + "89abcdef0123456789abcdef"
	msg := "git-glance shelf feature/login 1700000000"
	ok := [][]string{
		{"stash", "push", "--include-untracked", "--message", msg},
		{"stash", "push", "--include-untracked", "--message", "git-glance shelf main 0"},
		{"stash", "apply", "--index", sha1},
		{"stash", "apply", "--index", sha256},
		{"stash", "list", "--format=%H"},
		{"stash", "drop", "stash@{0}"},
		{"stash", "drop", "stash@{12}"},
		{"switch", "--no-guess", "--no-overwrite-ignore", "-c", "v2", "--track", "refs/remotes/origin/v2"},
		{"switch", "--no-guess", "--no-overwrite-ignore", "-c", "alice/feature/login", "--track", "refs/remotes/origin/alice/feature/login"},
	}
	for _, a := range ok {
		if err := Allowed(a); err != nil {
			t.Errorf("Allowed(%q) = %v, want nil", a, err)
		}
	}
	bad := [][]string{
		{"stash"},
		{"stash", "pop"},
		{"stash", "pop", "stash@{0}"},
		{"stash", "clear"},
		{"stash", "save", "x"},
		{"stash", "-u"},
		{"stash", "create"},
		{"stash", "store", sha1},
		{"stash", "branch", "x"},
		{"stash", "show", sha1},
		// push: exactly --include-untracked and a core-made message.
		{"stash", "push"},
		{"stash", "push", "--message", msg},
		{"stash", "push", "--all", "--message", msg},
		{"stash", "push", "--include-untracked", "--all", "--message", msg},
		{"stash", "push", "--all", "--include-untracked", "--message", msg},
		{"stash", "push", "-u", "-m", msg},
		{"stash", "push", "--include-untracked", "-m", msg},
		{"stash", "push", "--keep-index", "--message", msg},
		{"stash", "push", "--include-untracked", "--message"},
		{"stash", "push", "--include-untracked", "--message", msg, "--", "a.txt"},
		{"stash", "push", "--include-untracked", "--message", msg, "a.txt"},
		{"stash", "push", "--include-untracked", "--message", "my work"},
		{"stash", "push", "--include-untracked", "--message", "git-glance shelf main"},
		{"stash", "push", "--include-untracked", "--message", "git-glance shelf main 17x"},
		{"stash", "push", "--include-untracked", "--message", "git-glance shelf main -1"},
		{"stash", "push", "--include-untracked", "--message", "git-glance shelf -x 1"},
		{"stash", "push", "--include-untracked", "--message", "git-glance shelf a b 1"},
		{"stash", "push", "--include-untracked", "--message", "git-glance shelf  1"},
		{"stash", "push", "--include-untracked", "--message", "git-glance shelf main 1\n"},
		{"stash", "push", "--include-untracked", "--message=" + msg},
		// apply: a full lowercase hash, never a stash@{n} that may have shifted.
		{"stash", "apply"},
		{"stash", "apply", "stash@{0}"},
		{"stash", "apply", "--index", "stash@{0}"},
		{"stash", "apply", sha1},
		{"stash", "apply", "--index"},
		{"stash", "apply", "--index", sha1, "x"},
		{"stash", "apply", "--index", sha1[:39]},
		{"stash", "apply", "--index", sha1 + "0"},
		{"stash", "apply", "--index", "0123456789ABCDEF0123456789abcdef01234567"},
		{"stash", "apply", "--index", "0123456789abcdeg0123456789abcdef01234567"},
		{"stash", "apply", "--index", "HEAD"},
		{"stash", "apply", "--quiet", "--index", sha1},
		{"stash", "apply", sha1, "--index"},
		// list: exactly the hash format.
		{"stash", "list"},
		{"stash", "list", "-p"},
		{"stash", "list", "--format=%H", "-p"},
		{"stash", "list", "--format=%H", "--output=x"},
		{"stash", "list", "--format=%s"},
		// drop: one stash@{<n>} with decimal digits only.
		{"stash", "drop"},
		{"stash", "drop", "--all"},
		{"stash", "drop", "-q", "stash@{0}"},
		{"stash", "drop", "stash@{0}", "stash@{1}"},
		{"stash", "drop", sha1},
		{"stash", "drop", "stash@{}"},
		{"stash", "drop", "stash@{x}"},
		{"stash", "drop", "stash@{-1}"},
		{"stash", "drop", "stash@{+1}"},
		{"stash", "drop", "stash@{01}"},
		{"stash", "drop", "stash@{0"},
		{"stash", "drop", "stash@{0}x"},
		{"stash", "drop", "refs/stash@{0}"},
		{"stash", "drop", "stash@{1.hour.ago}"},
		{"stash", "drop", "stash@{1234567890}"},
		// switch -c: exactly --track to the remote-tracking ref of the same name.
		{"switch", "-C", "v2"},
		{"switch", "--force", "v2"},
		{"switch", "--no-guess", "--no-overwrite-ignore", "--force", "v2"},
		{"switch", "--no-guess", "--no-overwrite-ignore", "-c", "v2"},
		{"switch", "--no-guess", "--no-overwrite-ignore", "-c", "v2", "refs/remotes/origin/v2"},
		{"switch", "--no-guess", "--no-overwrite-ignore", "-C", "v2", "--track", "refs/remotes/origin/v2"},
		{"switch", "--no-guess", "--no-overwrite-ignore", "-c", "v2", "--track", "refs/heads/v2"},
		{"switch", "--no-guess", "--no-overwrite-ignore", "-c", "v2", "--track", "origin/v2"},
		{"switch", "--no-guess", "--no-overwrite-ignore", "-c", "v2", "--track", "refs/remotes/origin/v3"},
		{"switch", "--no-guess", "--no-overwrite-ignore", "-c", "v2", "--track", "refs/remotes//v2"},
		{"switch", "--no-guess", "--no-overwrite-ignore", "-c", "v2", "--track", "refs/remotes/v2"},
		{"switch", "--no-guess", "--no-overwrite-ignore", "-c", "v2", "--track", "refs/remotes/originv2"},
		{"switch", "--no-guess", "--no-overwrite-ignore", "-c", "v2", "--track=inherit", "refs/remotes/origin/v2"},
		{"switch", "--no-guess", "--no-overwrite-ignore", "-c", "v2", "--no-track", "refs/remotes/origin/v2"},
		{"switch", "--no-guess", "--no-overwrite-ignore", "-c", "v2", "--track", "refs/remotes/origin/v2", "x"},
		{"switch", "--no-guess", "--no-overwrite-ignore", "-c", "-x", "--track", "refs/remotes/origin/-x"},
		{"switch", "--no-guess", "--no-overwrite-ignore", "-c", "", "--track", "refs/remotes/origin/"},
		{"switch", "-c", "v2", "--track", "refs/remotes/origin/v2"},
		{"switch", "--no-guess", "-c", "v2", "--track", "refs/remotes/origin/v2"},
		{"switch", "--no-guess", "--no-overwrite-ignore", "--orphan", "v2"},
		{"switch", "--no-guess", "--no-overwrite-ignore", "--detach", "v2"},
		{"switch", "--no-guess", "--no-overwrite-ignore", "--track", "refs/remotes/origin/v2", "-c", "v2"},
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
