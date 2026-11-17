package gitx

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNotAllowed is returned (wrapped) for any git invocation outside the allowlist.
var ErrNotAllowed = errors.New("git command not allowed")

var readOnly = map[string]bool{
	"rev-parse": true, "for-each-ref": true, "rev-list": true, "log": true,
	"show": true, "cherry": true, "status": true, "diff": true,
	"merge-base": true, "patch-id": true, "ls-files": true, "version": true,
	"check-ref-format": true,
}

func deny(args []string, why string) error {
	return fmt.Errorf("%w: %s (%q)", ErrNotAllowed, why, args)
}

func eq(args []string, want ...string) bool {
	if len(args) != len(want) {
		return false
	}
	for i := range want {
		if args[i] != want[i] {
			return false
		}
	}
	return true
}

func plain(s string) bool { return s != "" && !strings.HasPrefix(s, "-") }

// Allowed reports whether args (an exact token list, never shell-interpreted)
// is a permitted git invocation. Mutating subcommands are allowed only in
// exact shapes; read-only subcommands take free arguments except abbreviations
// of --output. It returns nil or an error wrapping ErrNotAllowed.
func Allowed(args []string) error {
	if len(args) == 0 {
		return deny(args, "empty command")
	}
	sub := args[0]
	if !plain(sub) {
		return deny(args, "first argument must be a subcommand")
	}
	if readOnly[sub] {
		return allowedReadOnly(args)
	}
	ok := false
	switch sub {
	case "symbolic-ref":
		ok = len(args) == 4 && eq(args[:3], "symbolic-ref", "-q", "--short") && plain(args[3])
	case "fetch":
		ok = allowedFetch(args)
	case "merge":
		ok = len(args) == 4 && args[1] == "--ff-only" && args[2] == "--no-overwrite-ignore" && plain(args[3])
	case "switch":
		ok = len(args) == 3 && args[1] == "--no-guess" && plain(args[2])
	case "worktree":
		ok = allowedWorktree(args)
	}
	if !ok {
		return deny(args, "not an allowed shape")
	}
	return nil
}

// allowedInput permits the only command that may receive stdin.
func allowedInput(args []string) error {
	if eq(args, "patch-id", "--stable") {
		return nil
	}
	return deny(args, "command may not take stdin")
}

func allowedReadOnly(args []string) error {
	for _, a := range args[1:] {
		if !strings.HasPrefix(a, "--") {
			continue
		}
		name := strings.SplitN(a, "=", 2)[0]
		if len(name) >= 4 && strings.HasPrefix("--output", name) {
			return deny(args, "--output would write files")
		}
	}
	return nil
}

func allowedFetch(args []string) bool {
	if eq(args, "fetch", "--all", "--prune", "--no-prune-tags") {
		return true
	}
	if len(args) != 3 || args[1] != "." {
		return false
	}
	spec := args[2]
	if !plain(spec) || strings.HasPrefix(spec, "+") || strings.Count(spec, ":") != 1 || strings.Contains(spec, "*") {
		return false
	}
	src, dst, _ := strings.Cut(spec, ":")
	return strings.HasPrefix(src, "refs/remotes/") && strings.HasPrefix(dst, "refs/heads/")
}

func allowedWorktree(args []string) bool {
	switch {
	case eq(args, "worktree", "list", "--porcelain"):
		return true
	case len(args) == 7 && eq(args[:4], "worktree", "add", "--no-track", "-b"):
		return plain(args[4]) && plain(args[5]) && plain(args[6])
	case len(args) == 3 && eq(args[:2], "worktree", "remove"):
		return plain(args[2])
	}
	return false
}
