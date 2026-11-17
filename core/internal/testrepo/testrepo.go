// Package testrepo builds throwaway git repositories for tests. It calls the
// real git binary directly, bypassing the command allow-list, so it must only
// be used from tests.
package testrepo

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Repo is a bare "origin" plus a main working copy, all inside a temp dir.
type Repo struct {
	T      testing.TB
	Root   string // temp dir
	Dir    string // main worktree
	Remote string // bare repository path

	clock time.Time
}

const gitConfig = "[user]\n\tname = Test\n\temail = test@example.com\n" +
	"[init]\n\tdefaultBranch = main\n[core]\n\tautocrlf = false\n"

// New creates the bare remote, the working copy, and the first commit.
func New(t testing.TB) *Repo {
	t.Helper()
	root := filepath.ToSlash(t.TempDir())
	r := &Repo{
		T:      t,
		Root:   root,
		Dir:    root + "/work",
		Remote: root + "/origin.git",
		clock:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := os.MkdirAll(r.home(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.home()+"/.gitconfig", []byte(gitConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r.Git(root, "init", "--bare", r.Remote)
	r.Git(r.Dir, "init")
	r.Git(r.Dir, "remote", "add", "origin", r.Remote)
	r.commitWith(r.Dir, "init", map[string]string{"README": "init\n"}, r.clock)
	r.Git(r.Dir, "push", "-u", "origin", "main")
	r.Git(r.Dir, "remote", "set-head", "origin", "-a")
	return r
}

func (r *Repo) home() string { return r.Root + "/home" }

// Env returns the environment used by every helper: the inherited one without
// GIT_* variables, plus an isolated HOME and global config.
func (r *Repo) Env() []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GIT_") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"HOME="+r.home(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+r.home()+"/.gitconfig",
		"GIT_TERMINAL_PROMPT=0",
	)
	if runtime.GOOS == "windows" {
		env = append(env, "USERPROFILE="+r.home())
	}
	return env
}

// ApplyEnv sets the isolated config variables on the process for the rest of
// the test, so core code under test sees the same git configuration.
func (r *Repo) ApplyEnv() {
	r.T.Helper()
	r.T.Setenv("HOME", r.home())
	r.T.Setenv("GIT_CONFIG_GLOBAL", r.home()+"/.gitconfig")
	r.T.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if runtime.GOOS == "windows" {
		r.T.Setenv("USERPROFILE", r.home())
	}
}

// Clock returns the fake clock's current time (the last commit's time).
func (r *Repo) Clock() time.Time { return r.clock }

func (r *Repo) run(dir string, extraEnv []string, args ...string) string {
	r.T.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(r.Env(), extraEnv...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		r.T.Fatalf("git %s (in %s): %v\n%s", strings.Join(args, " "), dir, err, errb.String())
	}
	return strings.TrimSpace(out.String())
}

// Git runs git in dir and returns trimmed stdout; it fails the test on error.
func (r *Repo) Git(dir string, args ...string) string {
	r.T.Helper()
	return r.run(dir, nil, args...)
}

func (r *Repo) commitWith(dir, msg string, files map[string]string, t time.Time) string {
	r.T.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			r.T.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			r.T.Fatal(err)
		}
	}
	date := t.UTC().Format(time.RFC3339)
	env := []string{"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date}
	r.run(dir, env, "add", "-A")
	r.run(dir, env, "commit", "--allow-empty", "-m", msg)
	return r.run(dir, nil, "rev-parse", "HEAD")
}

// Commit writes files, stages everything, commits one fake hour after the
// previous commit, and returns the new hash.
func (r *Repo) Commit(dir, msg string, files map[string]string) string {
	r.T.Helper()
	r.clock = r.clock.Add(time.Hour)
	return r.commitWith(dir, msg, files, r.clock)
}

// CommitAt is like Commit but uses t as author and committer date and does
// not advance the fake clock.
func (r *Repo) CommitAt(dir, msg string, files map[string]string, t time.Time) string {
	r.T.Helper()
	return r.commitWith(dir, msg, files, t)
}

// Branch creates a branch in the main worktree.
func (r *Repo) Branch(name, from string) {
	r.T.Helper()
	r.Git(r.Dir, "branch", name, from)
}

// Push runs git push with args in dir.
func (r *Repo) Push(dir string, args ...string) {
	r.T.Helper()
	r.Git(dir, append([]string{"push"}, args...)...)
}

// AddWorktree checks out branch in a new folder Root/wt-<name>.
func (r *Repo) AddWorktree(name, branch string) string {
	r.T.Helper()
	p := r.Root + "/wt-" + name
	r.Git(r.Dir, "worktree", "add", p, branch)
	return p
}

// Clone makes a second clone of Remote at Root/<name>.
func (r *Repo) Clone(name string) string {
	r.T.Helper()
	p := r.Root + "/" + name
	r.Git(r.Root, "clone", r.Remote, p)
	return p
}
