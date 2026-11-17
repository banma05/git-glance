package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Runner executes allowlisted git commands. Every git call in the project goes through it.
type Runner interface {
	Run(ctx context.Context, dir string, args ...string) ([]byte, error)
	RunInput(ctx context.Context, dir string, input []byte, args ...string) ([]byte, error)
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

func gitEnv(base []string) []string {
	return append(base[:len(base):len(base)],
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=never",
		"SSH_ASKPASS_REQUIRE=never",
		"LC_ALL=C",
	)
}

// Run executes git with args in dir and returns stdout.
func (r ExecRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return r.run(ctx, dir, nil, args)
}

// RunInput is like Run but feeds input to the standard input of git.
func (r ExecRunner) RunInput(ctx context.Context, dir string, input []byte, args ...string) ([]byte, error) {
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
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = gitEnv(os.Environ())
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return stdout.Bytes(), &GitError{Args: args, ExitCode: ee.ExitCode(), Stderr: stderr.String()}
		}
		return stdout.Bytes(), err
	}
	return stdout.Bytes(), nil
}
