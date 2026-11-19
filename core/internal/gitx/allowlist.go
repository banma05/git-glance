package gitx

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrNotAllowed is returned (wrapped) for any git invocation outside the allowlist.
var ErrNotAllowed = errors.New("git command not allowed")

var readOnly = map[string]bool{
	"rev-parse": true, "for-each-ref": true, "rev-list": true, "log": true,
	"show": true, "cherry": true, "status": true, "diff": true,
	"merge-base": true, "patch-id": true, "ls-files": true, "version": true,
	"check-ref-format": true, "ls-tree": true,
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
		ok = len(args) == 5 && eq(args[:4], "merge", "--ff-only", "--no-overwrite-ignore", "--no-autostash") && plain(args[4])
	case "switch":
		ok = allowedSwitch(args)
	case "stash":
		ok = allowedStash(args)
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

// allowedSwitch permits switching to an existing local branch, and creating
// one that tracks the remote-tracking branch of the same name:
// switch --no-guess --no-overwrite-ignore -c <b> --track refs/remotes/<remote>/<b>.
func allowedSwitch(args []string) bool {
	if len(args) == 4 {
		return args[1] == "--no-guess" && args[2] == "--no-overwrite-ignore" && plain(args[3])
	}
	if len(args) != 7 || !eq(args[:4], "switch", "--no-guess", "--no-overwrite-ignore", "-c") || args[5] != "--track" {
		return false
	}
	branch, start := args[4], args[6]
	remote, ok := strings.CutPrefix(start, "refs/remotes/")
	if !plain(branch) || !ok {
		return false
	}
	remote, ok = strings.CutSuffix(remote, "/"+branch)
	return ok && remote != ""
}

// ShelfMessage is the stash message of a shelf: the only message the stash
// push shape accepts.
func ShelfMessage(branch string, unix int64) string {
	return fmt.Sprintf("%s%s %d", shelfPrefix, branch, unix)
}

// ParseShelfMessage reads the branch and time out of a shelf message.
func ParseShelfMessage(msg string) (branch string, unix int64, ok bool) {
	if !shelfMessage(msg) {
		return "", 0, false
	}
	branch, at, _ := strings.Cut(strings.TrimPrefix(msg, shelfPrefix), " ")
	unix, err := strconv.ParseInt(at, 10, 64)
	return branch, unix, err == nil
}

const shelfPrefix = "git-glance shelf "

// StoreMessage returns subject when the stash store shape accepts it as a
// message, else a fixed message.
func StoreMessage(subject string) string {
	if storeMessage(subject) {
		return subject
	}
	return "stored again by git-glance"
}

// allowedStash permits exactly: stash push --include-untracked --message
// <shelf message>, stash apply --index <full hash>, stash list --format=%H,
// stash drop stash@{<n>} and stash store --message <one line> <full hash>.
func allowedStash(args []string) bool {
	switch {
	case len(args) == 5 && eq(args[:4], "stash", "push", "--include-untracked", "--message"):
		return shelfMessage(args[4])
	case len(args) == 4 && eq(args[:3], "stash", "apply", "--index"):
		return fullHash(args[3])
	case eq(args, "stash", "list", "--format=%H"):
		return true
	case len(args) == 3 && args[1] == "drop":
		n, ok := strings.CutPrefix(args[2], "stash@{")
		n, ok2 := strings.CutSuffix(n, "}")
		return ok && ok2 && decimal(n, 9)
	case len(args) == 5 && eq(args[:3], "stash", "store", "--message"):
		return storeMessage(args[3]) && fullHash(args[4])
	}
	return false
}

// shelfMessage accepts "git-glance shelf <branch> <unix time>" only.
func shelfMessage(msg string) bool {
	rest, ok := strings.CutPrefix(msg, shelfPrefix)
	if !ok {
		return false
	}
	parts := strings.Split(rest, " ")
	return len(parts) == 2 && plain(parts[0]) && !strings.ContainsFunc(parts[0], control) && decimal(parts[1], 18)
}

// storeMessage accepts one line of at most 1000 bytes that does not start
// with "-".
func storeMessage(msg string) bool {
	return plain(msg) && len(msg) <= 1000 && !strings.ContainsFunc(msg, control)
}

func control(r rune) bool { return r < 0x20 || r == 0x7f }

// decimal accepts 1 to limit digits without a leading zero, or "0".
func decimal(s string, limit int) bool {
	if s == "" || len(s) > limit || (len(s) > 1 && s[0] == '0') {
		return false
	}
	return strings.Trim(s, "0123456789") == ""
}

// fullHash accepts a full lowercase SHA-1 or SHA-256 object name.
func fullHash(s string) bool {
	return (len(s) == 40 || len(s) == 64) && strings.Trim(s, "0123456789abcdef") == ""
}

func allowedWorktree(args []string) bool {
	switch {
	case eq(args, "worktree", "list", "--porcelain"):
		return true
	case len(args) == 7 && eq(args[:4], "worktree", "add", "--no-track", "-b"):
		return plain(args[4]) && plain(args[5]) && plain(args[6])
	case len(args) == 4 && eq(args[:2], "worktree", "add"):
		// worktree add <path> <branch>: an existing local branch only; the
		// caller checks that it exists, as git would otherwise create one.
		return plain(args[2]) && plain(args[3])
	case len(args) == 3 && eq(args[:2], "worktree", "remove"):
		return plain(args[2])
	}
	return false
}
