package model

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/repo"
	"github.com/banma05/git-glance/core/internal/settings"
	"github.com/banma05/git-glance/core/internal/testrepo"
)

// countingRunner counts the git processes a view starts, and their time per
// subcommand.
type countingRunner struct {
	gitx.ExecRunner
	n     atomic.Int64
	mu    sync.Mutex
	calls map[string]int
	spent map[string]time.Duration
}

func (c *countingRunner) note(sub string, since time.Time) {
	c.n.Add(1)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.calls == nil {
		c.calls, c.spent = map[string]int{}, map[string]time.Duration{}
	}
	c.calls[sub]++
	c.spent[sub] += time.Since(since)
}

func (c *countingRunner) reset() {
	c.n.Store(0)
	c.mu.Lock()
	c.calls, c.spent = nil, nil
	c.mu.Unlock()
}

func (c *countingRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	defer c.note(args[0], time.Now())
	return c.ExecRunner.Run(ctx, dir, args...)
}

func (c *countingRunner) RunInput(ctx context.Context, dir string, input []byte, args ...string) ([]byte, error) {
	defer c.note(args[0], time.Now())
	return c.ExecRunner.RunInput(ctx, dir, input, args...)
}

func (c *countingRunner) breakdown() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	subs := make([]string, 0, len(c.calls))
	for s := range c.calls {
		subs = append(subs, s)
	}
	sort.Slice(subs, func(i, j int) bool { return c.spent[subs[i]] > c.spent[subs[j]] })
	var b strings.Builder
	for _, s := range subs {
		fmt.Fprintf(&b, " %s=%d/%v", s, c.calls[s], c.spent[s].Round(time.Millisecond))
	}
	return b.String()
}

const (
	perfMain     = 5000 // commits on main
	perfV2       = 300  // commits on v2, cut from main
	perfFeatures = 300  // local branches, two own commits each
	perfRemotes  = 3000 // remote-tracking branches
)

// perfStream is a fast-import stream for the synthetic repository.
func perfStream() []byte {
	var b bytes.Buffer
	ts := int64(1700000000)
	mark := 0
	commit := func(ref, from, path string) int {
		mark++
		ts += 60
		fmt.Fprintf(&b, "commit %s\nmark :%d\ncommitter T <t@example.com> %d +0000\n", ref, mark, ts)
		msg := fmt.Sprintf("change %d", mark)
		fmt.Fprintf(&b, "data %d\n%s\n", len(msg), msg)
		if from != "" {
			fmt.Fprintf(&b, "from %s\n", from)
		}
		body := fmt.Sprintf("%d\n", mark)
		fmt.Fprintf(&b, "M 644 inline %s\ndata %d\n%s\n", path, len(body), body)
		return mark
	}
	commit("refs/heads/main", "refs/heads/main^0", "trunk/f0.txt")
	for i := 1; i < perfMain; i++ {
		commit("refs/heads/main", "", fmt.Sprintf("trunk/f%d.txt", i%100))
	}
	cut := perfMain - 500
	v2 := commit("refs/heads/v2", fmt.Sprintf(":%d", cut), "v2/f0.txt")
	for i := 1; i < perfV2; i++ {
		v2 = commit("refs/heads/v2", "", fmt.Sprintf("v2/f%d.txt", i%50))
	}
	for k := 0; k < perfFeatures; k++ {
		ref := fmt.Sprintf("refs/heads/alice/f%03d", k)
		fork := perfMain - 1 - k%200
		if k%2 == 1 {
			fork = v2 - k%100
		}
		commit(ref, fmt.Sprintf(":%d", fork), fmt.Sprintf("feat/%d-a.txt", k))
		commit(ref, "", fmt.Sprintf("feat/%d-b.txt", k))
	}
	for n := 0; n < perfRemotes; n++ {
		fmt.Fprintf(&b, "reset refs/remotes/origin/r%04d\nfrom :%d\n\n", n, 1+(n*37)%perfMain)
	}
	fmt.Fprintf(&b, "reset refs/remotes/origin/main\nfrom :%d\n\n", perfMain)
	fmt.Fprintf(&b, "reset refs/remotes/origin/v2\nfrom :%d\n\n", v2)
	return b.Bytes()
}

// TestPerfView times a view of a large synthetic repository. It is opt-in:
// GITGLANCE_PERF=1 go test ./internal/model -run TestPerfView -v -timeout 30m
func TestPerfView(t *testing.T) {
	if os.Getenv("GITGLANCE_PERF") == "" {
		t.Skip("set GITGLANCE_PERF=1 to run the performance check")
	}
	r := testrepo.New(t)
	r.ApplyEnv()
	started := time.Now()
	cmd := exec.Command("git", "fast-import", "--quiet", "--force")
	cmd.Dir, cmd.Env, cmd.Stdin = r.Dir, r.Env(), bytes.NewReader(perfStream())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fast-import: %v\n%s", err, out)
	}
	r.Git(r.Dir, "reset", "-q", "--hard", "main")
	r.Git(r.Dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	t.Logf("synthetic repo built in %v", time.Since(started).Round(time.Millisecond))

	ctx := context.Background()
	run := &countingRunner{}
	ver, err := gitx.DetectVersion(ctx, run)
	if err != nil {
		t.Fatal(err)
	}
	rs := settings.RepoSettings{Trunks: []settings.TrunkConfig{{Name: "main"}, {Name: "v2"}}}
	for pass := 1; pass <= 2; pass++ {
		run.reset()
		t0 := time.Now()
		snap, err := repo.Read(ctx, run, r.Dir)
		if err != nil {
			t.Fatal(err)
		}
		readTime, readCalls := time.Since(t0), run.n.Load()
		t1 := time.Now()
		v, err := Build(ctx, run, ver, snap, rs, time.Now().Unix())
		if err != nil {
			t.Fatal(err)
		}
		buildTime := time.Since(t1)
		t.Logf("pass %d: %d local, %d remote refs; Read %v (%d git), Build %v (%d git), total %v; branches %d",
			pass, len(snap.Local), len(snap.Remote), readTime.Round(time.Millisecond), readCalls,
			buildTime.Round(time.Millisecond), run.n.Load()-readCalls, time.Since(t0).Round(time.Millisecond), len(v.Branches))
		t.Logf("pass %d git calls/time by subcommand:%s", pass, run.breakdown())
	}
}
