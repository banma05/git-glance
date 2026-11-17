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

func contains(args []string, vals ...string) bool {
	for _, a := range args {
		for _, v := range vals {
			if a == v {
				return true
			}
		}
	}
	return false
}

func deny(args []string, why string) error {
	return fmt.Errorf("%w: %s (%q)", ErrNotAllowed, why, args)
}

// Allowed reports whether args (an exact token list, never shell-interpreted)
// is a permitted git invocation. It returns nil or an error wrapping ErrNotAllowed.
func Allowed(args []string) error {
	if len(args) == 0 {
		return deny(args, "empty command")
	}
	sub := args[0]
	if sub == "" || strings.HasPrefix(sub, "-") {
		return deny(args, "first argument must be a subcommand")
	}
	if readOnly[sub] {
		return nil
	}
	switch sub {
	case "symbolic-ref":
		if len(args) == 4 && args[1] == "-q" && args[2] == "--short" && args[3] != "" {
			return nil
		}
		return deny(args, "symbolic-ref is read-only only")
	case "fetch":
		return allowedFetch(args)
	case "merge":
		if contains(args[1:], "--ff-only") {
			return nil
		}
		return deny(args, "merge requires --ff-only")
	case "switch":
		if len(args) == 2 && args[1] != "" && !strings.HasPrefix(args[1], "-") {
			return nil
		}
		return deny(args, "switch takes exactly one branch name")
	case "worktree":
		return allowedWorktree(args)
	}
	return deny(args, "subcommand not in allowlist")
}

func allowedFetch(args []string) error {
	for _, a := range args[1:] {
		if a == "--force" || a == "-f" || strings.HasPrefix(a, "+") {
			return deny(args, "forced fetch")
		}
	}
	if contains(args[1:], "--all") {
		return nil
	}
	if len(args) == 3 && args[1] == "." {
		return nil
	}
	return deny(args, "fetch must be --all or local fetch of one refspec")
}

func allowedWorktree(args []string) error {
	if len(args) < 2 {
		return deny(args, "worktree needs an action")
	}
	switch args[1] {
	case "list":
		return nil
	case "add":
		if contains(args[2:], "-B", "--force", "-f") {
			return deny(args, "worktree add with force")
		}
		return nil
	case "remove":
		if contains(args[2:], "--force", "-f") {
			return deny(args, "worktree remove with force")
		}
		return nil
	}
	return deny(args, "worktree action not in allowlist")
}
