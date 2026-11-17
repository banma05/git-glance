package model

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/repo"
	"github.com/banma05/git-glance/core/internal/settings"
	"github.com/banma05/git-glance/core/internal/testrepo"
)

// recordingRunner runs git through ExecRunner and remembers every argument list.
type recordingRunner struct {
	gitx.ExecRunner
	mu    sync.Mutex
	calls [][]string
}

func (r *recordingRunner) record(args []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, append([]string(nil), args...))
}

func (r *recordingRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	r.record(args)
	return r.ExecRunner.Run(ctx, dir, args...)
}

func (r *recordingRunner) RunInput(ctx context.Context, dir string, input []byte, args ...string) ([]byte, error) {
	r.record(args)
	return r.ExecRunner.RunInput(ctx, dir, input, args...)
}

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// With log.showSignature=true, log and show print signature lines into
// their output; every such call must turn that off.
func TestBuildIgnoresShowSignature(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	r.Git(r.Dir, "config", "log.showSignature", "true")
	r.Branch("main-test", "main")
	r.Push(r.Dir, "origin", "main-test")
	r.Branch("feature/x", "main")
	r.Git(r.Dir, "checkout", "-q", "feature/x")
	r.Commit(r.Dir, "x", map[string]string{"x.txt": "x\n"})
	r.Git(r.Dir, "checkout", "-q", "main")
	r.Commit(r.Dir, "main work", map[string]string{"x.txt": "other\n"})
	r.Push(r.Dir, "origin", "main")
	now := r.Clock().Add(time.Hour).Unix()

	ctx := context.Background()
	rec := &recordingRunner{}
	v, err := gitx.DetectVersion(ctx, rec)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := repo.Read(ctx, rec, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	rec.calls = nil
	rs := settings.RepoSettings{Trunks: []settings.TrunkConfig{{Name: "main", Test: "main-test"}}}
	if _, err := Build(ctx, rec, v, snap, rs, now); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, args := range rec.calls {
		if args[0] != "log" && args[0] != "show" {
			continue
		}
		if !hasArg(args, "--no-show-signature") {
			t.Errorf("missing --no-show-signature: git %s", strings.Join(args, " "))
		}
		for _, shape := range []string{"--format=%ct", logFields, "--format=%s", "-p"} {
			if hasArg(args, shape) {
				seen[shape] = true
			}
		}
	}
	// Every log call in the model package must have run at least once.
	if len(seen) != 4 {
		t.Fatalf("log shapes exercised: %v", seen)
	}
}
