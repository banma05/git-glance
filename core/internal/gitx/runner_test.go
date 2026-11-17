package gitx

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRunRefusesWithoutSpawning(t *testing.T) {
	r := ExecRunner{Git: "definitely-not-a-binary"}
	_, err := r.Run(context.Background(), t.TempDir(), "push")
	if !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("Run err = %v, want ErrNotAllowed", err)
	}
	_, err = r.RunInput(context.Background(), t.TempDir(), []byte("x"), "push")
	if !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("RunInput err = %v, want ErrNotAllowed", err)
	}
}

func TestGitEnv(t *testing.T) {
	env := gitEnv(nil)
	want := []string{"GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "SSH_ASKPASS_REQUIRE=never", "LC_ALL=C"}
	for _, w := range want {
		found := false
		for _, e := range env {
			if e == w {
				found = true
			}
		}
		if !found {
			t.Errorf("gitEnv missing %q in %v", w, env)
		}
	}
}

func TestRunGitError(t *testing.T) {
	r := ExecRunner{}
	_, err := r.Run(context.Background(), t.TempDir(), "rev-parse", "--verify", "refs/heads/nope")
	var ge *GitError
	if !errors.As(err, &ge) || ge.ExitCode == 0 {
		t.Fatalf("err = %v, want *GitError with non-zero exit", err)
	}
	if !strings.Contains(ge.Error(), "rev-parse") {
		t.Errorf("GitError text %q lacks args", ge.Error())
	}
}

func TestRunInput(t *testing.T) {
	r := ExecRunner{}
	diff := "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1 +1 @@\n-a\n+b\n"
	out, err := r.RunInput(context.Background(), t.TempDir(), []byte(diff), "patch-id", "--stable")
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.Fields(string(out))) != 2 {
		t.Errorf("patch-id output %q", out)
	}
}

func TestDetectVersion(t *testing.T) {
	v, err := DetectVersion(context.Background(), ExecRunner{})
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
