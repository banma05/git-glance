package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Runner executes allowlisted git commands. Every git call in the project goes through it.
type Runner interface {
	Run(ctx context.Context, dir string, args ...string) ([]byte, error)
	RunInput(ctx context.Context, dir string, input []byte, args ...string) ([]byte, error)
	// RunCapped runs a read-only command and keeps at most limit bytes of
	// stdout. Past that the process is killed and truncated is true.
	RunCapped(ctx context.Context, dir string, limit int64, args ...string) (out []byte, truncated bool, err error)
}

// ExecRunner runs git as a child process (no shell) after checking Allowed.
type ExecRunner struct {
	Git     string        // default "git"
	Timeout time.Duration // default 60s
}

// GitError is returned when git exits with a non-zero status.
type GitError struct {
	Args     []string
	ExitCode int
	Stderr   string
}

func (e *GitError) Error() string {
	return fmt.Sprintf("git %s: exit %d: %s", strings.Join(e.Args, " "), e.ExitCode, strings.TrimSpace(e.Stderr))
}

var strippedEnv = map[string]bool{
	"GIT_DIR": true, "GIT_WORK_TREE": true, "GIT_INDEX_FILE": true, "GIT_COMMON_DIR": true,
	"GIT_NAMESPACE": true,
	"GIT_EXEC_PATH": true, "GIT_CONFIG_PARAMETERS": true, "GIT_CONFIG_COUNT": true,
	// Set below, so inherited values must not survive.
	"GIT_TERMINAL_PROMPT": true, "GCM_INTERACTIVE": true, "SSH_ASKPASS_REQUIRE": true,
	"LC_ALL": true, "GIT_OPTIONAL_LOCKS": true,
}

func stripped(entry string) bool {
	name, _, _ := strings.Cut(entry, "=")
	name = strings.ToUpper(name)
	return strippedEnv[name] || strings.HasPrefix(name, "GIT_OBJECT_") || strings.HasPrefix(name, "GIT_ALTERNATE_OBJECT_") ||
		strings.HasPrefix(name, "GIT_CONFIG_KEY_") || strings.HasPrefix(name, "GIT_CONFIG_VALUE_")
}

// gitEnv returns base without variables that could retarget git or override
// the settings below, plus those settings.
func gitEnv(base []string) []string {
	env := make([]string, 0, len(base)+5)
	for _, e := range base {
		if !stripped(e) {
			env = append(env, e)
		}
	}
	return append(env,
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=never",
		"SSH_ASKPASS_REQUIRE=never",
		"LC_ALL=C",
		"GIT_OPTIONAL_LOCKS=0",
	)
}

const (
	localMutationTimeout = 30 * time.Minute
	networkFetchTimeout  = 15 * time.Minute
)

// localMutation reports whether args change the local repository only. Such
// commands must not be interrupted half way. args has passed Allowed.
func localMutation(args []string) bool {
	switch args[0] {
	case "merge", "switch", "worktree", "stash":
		return !(args[0] == "worktree" && args[1] == "list") && !(args[0] == "stash" && args[1] == "list")
	case "fetch":
		return args[1] == "."
	}
	return false
}

// timeoutFor returns how long args may run: Timeout (default 60s) for reads,
// at least 15 minutes for the network fetch (a first fetch of a large
// repository over a slow link) and at least 30 minutes for a local mutation (a
// worktree add of a large repository can take minutes). args has passed
// Allowed.
func (r ExecRunner) timeoutFor(args []string) time.Duration {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	switch {
	case localMutation(args):
		timeout = max(timeout, localMutationTimeout)
	case args[0] == "fetch":
		timeout = max(timeout, networkFetchTimeout)
	}
	return timeout
}

// Run executes git with args in dir and returns stdout.
func (r ExecRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return r.run(ctx, dir, nil, args)
}

// RunInput is like Run but feeds input to the standard input of git.
func (r ExecRunner) RunInput(ctx context.Context, dir string, input []byte, args ...string) ([]byte, error) {
	if err := allowedInput(args); err != nil {
		return nil, err
	}
	return r.run(ctx, dir, input, args)
}

func (r ExecRunner) run(ctx context.Context, dir string, input []byte, args []string) ([]byte, error) {
	if err := Allowed(args); err != nil {
		return nil, err
	}
	bin := r.Git
	if bin == "" {
		bin = "git"
	}
	if localMutation(args) {
		// A killed git leaves stale lock files and half-updated folders.
		ctx = context.WithoutCancel(ctx)
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeoutFor(args))
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.WaitDelay = 5 * time.Second
	hideConsole(cmd)
	cmd.Env = gitEnv(os.Environ())
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return stdout.Bytes(), fmt.Errorf("git %s: %w", strings.Join(args, " "), cerr)
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return stdout.Bytes(), &GitError{Args: append([]string(nil), args...), ExitCode: ee.ExitCode(), Stderr: stderr.String()}
		}
		return stdout.Bytes(), err
	}
	return stdout.Bytes(), nil
}

// RunCapped is like Run for read-only commands only, but reads at most limit
// bytes of stdout. When the output is longer, git is killed (safe: it only
// reads, and GIT_OPTIONAL_LOCKS=0 is set) and truncated is true. Commands
// that change anything are refused with ErrNotAllowed.
func (r ExecRunner) RunCapped(ctx context.Context, dir string, limit int64, args ...string) ([]byte, bool, error) {
	if err := Allowed(args); err != nil {
		return nil, false, err
	}
	if localMutation(args) || !readOnly[args[0]] {
		return nil, false, deny(args, "RunCapped takes read-only commands only")
	}
	bin := r.Git
	if bin == "" {
		bin = "git"
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.WaitDelay = 5 * time.Second
	hideConsole(cmd)
	cmd.Env = gitEnv(os.Environ())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, err
	}
	if err := cmd.Start(); err != nil {
		return nil, false, err
	}
	out, readErr := io.ReadAll(io.LimitReader(pipe, limit+1))
	truncated := int64(len(out)) > limit
	if truncated {
		out = out[:limit]
		cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if truncated {
		return out, true, nil
	}
	if cerr := ctx.Err(); cerr != nil {
		return out, false, fmt.Errorf("git %s: %w", strings.Join(args, " "), cerr)
	}
	if readErr != nil {
		return out, false, readErr
	}
	var ee *exec.ExitError
	if errors.As(waitErr, &ee) {
		return out, false, &GitError{Args: append([]string(nil), args...), ExitCode: ee.ExitCode(), Stderr: stderr.String()}
	}
	return out, false, waitErr
}
