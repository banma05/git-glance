package gitx

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func initRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "repo")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = dir
	cmd.Env = gitEnv(os.Environ())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	return dir
}

func TestRunRefusesWithoutSpawning(t *testing.T) {
	r := ExecRunner{Git: "definitely-not-a-binary"}
	_, err := r.Run(context.Background(), t.TempDir(), "push")
	if !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("Run err = %v, want ErrNotAllowed", err)
	}
	for _, a := range [][]string{{"push"}, {"fetch", ".", "--stdin"}, {"fetch", "--all", "--stdin"}, {"log"}} {
		_, err = r.RunInput(context.Background(), t.TempDir(), []byte("+x:y"), a...)
		if !errors.Is(err, ErrNotAllowed) {
			t.Fatalf("RunInput(%q) err = %v, want ErrNotAllowed", a, err)
		}
	}
}

func TestGitEnv(t *testing.T) {
	base := []string{
		"PATH=/bin", "HOME=/h", "GIT_CONFIG_GLOBAL=/g", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_DIR=/x", "GIT_WORK_TREE=/x", "GIT_INDEX_FILE=/x", "GIT_COMMON_DIR=/x",
		"GIT_OBJECT_X=1", "GIT_ALTERNATE_OBJECT_Y=1", "GIT_NAMESPACE=x",
		"GIT_EXEC_PATH=/x", "GIT_CONFIG_PARAMETERS='a=b'", "GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=a", "GIT_CONFIG_VALUE_0=b", "LC_ALL=de_DE",
	}
	env := gitEnv(base)
	has := func(e string) bool {
		for _, v := range env {
			if v == e {
				return true
			}
		}
		return false
	}
	for _, w := range []string{"PATH=/bin", "HOME=/h", "GIT_CONFIG_GLOBAL=/g", "GIT_CONFIG_NOSYSTEM=1"} {
		if !has(w) {
			t.Errorf("base var %q lost: %v", w, env)
		}
	}
	for _, w := range []string{"GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "SSH_ASKPASS_REQUIRE=never", "LC_ALL=C", "GIT_OPTIONAL_LOCKS=0"} {
		if !has(w) {
			t.Errorf("missing %q: %v", w, env)
		}
	}
	for _, e := range env {
		name := strings.SplitN(e, "=", 2)[0]
		switch name {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_X",
			"GIT_ALTERNATE_OBJECT_Y", "GIT_NAMESPACE", "GIT_EXEC_PATH", "GIT_CONFIG_PARAMETERS",
			"GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0":
			t.Errorf("stripped var present: %q", e)
		}
	}
	if has("LC_ALL=de_DE") {
		t.Error("inherited LC_ALL not overridden")
	}
}

func TestRunTimeoutKeepsCause(t *testing.T) {
	dir := initRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ExecRunner{}.Run(ctx, dir, "status")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestGitErrorCopiesArgs(t *testing.T) {
	dir := initRepo(t)
	args := []string{"rev-parse", "--verify", "refs/heads/nope"}
	_, err := ExecRunner{}.Run(context.Background(), dir, args...)
	args[0] = "mutated"
	var ge *GitError
	if !errors.As(err, &ge) || ge.Args[0] != "rev-parse" {
		t.Fatalf("GitError.Args aliased caller slice: %v", err)
	}
}

func TestRunGitError(t *testing.T) {
	r := ExecRunner{}
	_, err := r.Run(context.Background(), initRepo(t), "rev-parse", "--verify", "refs/heads/nope")
	var ge *GitError
	if !errors.As(err, &ge) || ge.ExitCode == 0 {
		t.Fatalf("err = %v, want *GitError with non-zero exit", err)
	}
	if !strings.Contains(ge.Stderr, "nope") && !strings.Contains(ge.Stderr, "Needed a single revision") {
		t.Errorf("unexpected stderr %q", ge.Stderr)
	}
	if !strings.Contains(ge.Error(), "rev-parse") {
		t.Errorf("GitError text %q lacks args", ge.Error())
	}
}

func TestRunInput(t *testing.T) {
	r := ExecRunner{}
	diff := "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1 +1 @@\n-a\n+b\n"
	out, err := r.RunInput(context.Background(), initRepo(t), []byte(diff), "patch-id", "--stable")
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.Fields(string(out))) != 2 {
		t.Errorf("patch-id output %q", out)
	}
}

type dirRunner struct {
	Runner
	dir string
}

func (d dirRunner) Run(ctx context.Context, _ string, args ...string) ([]byte, error) {
	return d.Runner.Run(ctx, d.dir, args...)
}

func TestDetectVersion(t *testing.T) {
	v, err := DetectVersion(context.Background(), dirRunner{ExecRunner{}, t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if !v.AtLeast(2, 30) {
		t.Errorf("version %+v < 2.30", v)
	}
	cases := []struct {
		v    Version
		want bool
	}{
		{Version{2, 40}, false}, {Version{2, 41}, true}, {Version{3, 0}, true},
	}
	for _, c := range cases {
		if got := c.v.AtLeast(2, 41); got != c.want {
			t.Errorf("%+v.AtLeast(2,41) = %v, want %v", c.v, got, c.want)
		}
	}
}

func TestParseVersion(t *testing.T) {
	for in, want := range map[string]Version{
		"git version 2.43.0\n":                 {2, 43},
		"git version 2.45.1.windows.1\n":       {2, 45},
		"git version 2.39.3 (Apple Git-145)\n": {2, 39},
	} {
		got, err := parseVersion(in)
		if err != nil || got != want {
			t.Errorf("parseVersion(%q) = %+v, %v", in, got, err)
		}
	}
	if _, err := parseVersion("nonsense"); err == nil {
		t.Error("want error for nonsense")
	}
}

func TestLocalMutationsIgnoreCancellation(t *testing.T) {
	dir := initRepo(t)
	for _, args := range [][]string{
		{"-c", "user.name=T", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "init"},
		{"branch", "other"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = gitEnv(os.Environ())
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := ExecRunner{}
	if _, err := r.Run(ctx, dir, "rev-parse", "HEAD"); err == nil {
		t.Fatal("read-only command with a cancelled context must fail")
	}
	if _, err := r.Run(ctx, dir, "switch", "--no-guess", "--no-overwrite-ignore", "other"); err != nil {
		t.Fatalf("local mutating command must run to completion: %v", err)
	}
	out, err := r.Run(context.Background(), dir, "rev-parse", "--symbolic-full-name", "HEAD")
	if err != nil || strings.TrimSpace(string(out)) != "refs/heads/other" {
		t.Fatalf("HEAD = %q, %v; want refs/heads/other", out, err)
	}
}

func TestRunCappedRefusesMutations(t *testing.T) {
	r := ExecRunner{Git: "definitely-not-a-binary"}
	for _, a := range [][]string{{"push"}, {"fetch", "--all", "--prune", "--no-prune-tags"}, {"merge", "--ff-only", "--no-overwrite-ignore", "--no-autostash", "x"}, {"worktree", "remove", "x"}, {"symbolic-ref", "-q", "--short", "HEAD"}, {"stash", "list", "--format=%H"}, {"stash", "drop", "stash@{0}"}} {
		_, _, err := r.RunCapped(context.Background(), t.TempDir(), 10, a...)
		if !errors.Is(err, ErrNotAllowed) {
			t.Fatalf("RunCapped(%q) err = %v, want ErrNotAllowed", a, err)
		}
	}
}

func TestRunCappedTruncates(t *testing.T) {
	dir := initRepo(t)
	big := strings.Repeat("0123456789", 10000)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	r := ExecRunner{}
	out, trunc, err := r.RunCapped(context.Background(), dir, 1000, "diff", "--no-index", "--no-color", "--", os.DevNull, "f.txt")
	var ge *GitError
	if errors.As(err, &ge) && ge.ExitCode == 1 {
		err = nil // diff --no-index exits 1 when the files differ
	}
	if err != nil || !trunc || len(out) != 1000 {
		t.Fatalf("out %d bytes, truncated %v, err %v", len(out), trunc, err)
	}
	out, trunc, err = r.RunCapped(context.Background(), dir, 1<<20, "version")
	if err != nil || trunc || !strings.HasPrefix(string(out), "git version") {
		t.Fatalf("small output: %q %v %v", out, trunc, err)
	}
	if _, _, err = r.RunCapped(context.Background(), dir, 10, "rev-parse", "--verify", "--quiet", "nope^{commit}"); !errors.As(err, &ge) {
		t.Fatalf("want GitError, got %v", err)
	}
}

func TestTimeouts(t *testing.T) {
	r := ExecRunner{}
	cases := []struct {
		args []string
		want time.Duration
	}{
		{[]string{"status"}, 60 * time.Second},
		{[]string{"fetch", "--all", "--prune", "--no-prune-tags"}, 15 * time.Minute},
		{[]string{"fetch", ".", "refs/remotes/origin/x:refs/heads/x"}, 30 * time.Minute},
		{[]string{"worktree", "remove", "p"}, 30 * time.Minute},
		{[]string{"stash", "push", "--include-untracked", "--message", "git-glance shelf main 1"}, 30 * time.Minute},
		{[]string{"stash", "apply", "--index", "0123456789abcdef0123456789abcdef01234567"}, 30 * time.Minute},
		{[]string{"stash", "drop", "stash@{0}"}, 30 * time.Minute},
		{[]string{"stash", "list", "--format=%H"}, 60 * time.Second},
	}
	for _, c := range cases {
		if got := r.timeoutFor(c.args); got != c.want {
			t.Errorf("timeoutFor(%q) = %v, want %v", c.args, got, c.want)
		}
	}
	if got := (ExecRunner{Timeout: time.Hour}).timeoutFor(cases[1].args); got != time.Hour {
		t.Errorf("a longer configured timeout is kept: %v", got)
	}
}
