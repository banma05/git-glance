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

// perfShape sizes a synthetic repository: commits on main, commits on v2
// (cut from main), local feature branches with two own commits each (half on
// main, half on v2; every third one pushed) and other remote-tracking
// branches.
type perfShape struct {
	main, v2, features, remotes int
}

// perfStream is a fast-import stream for the shape. It also returns the
// time of the newest commit.
func perfStream(s perfShape) ([]byte, int64) {
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
	for i := 1; i < s.main; i++ {
		commit("refs/heads/main", "", fmt.Sprintf("trunk/f%d.txt", i%100))
	}
	v2 := commit("refs/heads/v2", fmt.Sprintf(":%d", s.main-s.main/10), "v2/f0.txt")
	for i := 1; i < s.v2; i++ {
		v2 = commit("refs/heads/v2", "", fmt.Sprintf("v2/f%d.txt", i%50))
	}
	for k := 0; k < s.features; k++ {
		name := fmt.Sprintf("alice/f%03d", k)
		fork := s.main - 1 - k%200
		if k%2 == 1 {
			fork = v2 - k%min(100, s.v2)
		}
		commit("refs/heads/"+name, fmt.Sprintf(":%d", fork), fmt.Sprintf("feat/%d-a.txt", k))
		tip := commit("refs/heads/"+name, "", fmt.Sprintf("feat/%d-b.txt", k))
		if k%3 == 0 {
			fmt.Fprintf(&b, "reset refs/remotes/origin/%s\nfrom :%d\n\n", name, tip)
		}
	}
	for n := 0; n < s.remotes; n++ {
		fmt.Fprintf(&b, "reset refs/remotes/origin/r%04d\nfrom :%d\n\n", n, 1+(n*37)%s.main)
	}
	fmt.Fprintf(&b, "reset refs/remotes/origin/main\nfrom :%d\n\n", s.main)
	fmt.Fprintf(&b, "reset refs/remotes/origin/v2\nfrom :%d\n\n", v2)
	return b.Bytes(), ts
}

// perfRepo builds the synthetic repository of the shape with fast-import and
// packs its refs, as a maintained repository has them. It returns the repo,
// its settings and a "now" just after the newest commit, so that the
// branches are recent and drawn in the graph.
func perfRepo(t *testing.T, s perfShape) (*testrepo.Repo, settings.RepoSettings, int64) {
	t.Helper()
	r := testrepo.New(t)
	r.ApplyEnv()
	stream, last := perfStream(s)
	cmd := exec.Command("git", "fast-import", "--quiet", "--force")
	cmd.Dir, cmd.Env, cmd.Stdin = r.Dir, r.Env(), bytes.NewReader(stream)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fast-import: %v\n%s", err, out)
	}
	r.Git(r.Dir, "reset", "-q", "--hard", "main")
	r.Git(r.Dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	r.Git(r.Dir, "pack-refs", "--all")
	return r, settings.RepoSettings{Trunks: []settings.TrunkConfig{{Name: "main"}, {Name: "v2"}}}, last + 3600
}

// TestPerfView times the first view and an unchanged refresh of large
// synthetic repositories. It is opt-in:
// GITGLANCE_PERF=1 go test ./internal/model -run TestPerfView -v -timeout 30m
func TestPerfView(t *testing.T) {
	if os.Getenv("GITGLANCE_PERF") == "" {
		t.Skip("set GITGLANCE_PERF=1 to run the performance check")
	}
	for _, c := range []struct {
		name  string
		shape perfShape
	}{
		{"wide", perfShape{main: 5000, v2: 300, features: 300, remotes: 3000}},
		{"long", perfShape{main: 39000, v2: 300, features: 20, remotes: 3800}},
	} {
		t.Run(c.name, func(t *testing.T) {
			started := time.Now()
			r, rs, now := perfRepo(t, c.shape)
			t.Logf("%+v built in %v", c.shape, time.Since(started).Round(time.Millisecond))
			ctx := context.Background()
			run := &countingRunner{}
			ver, err := gitx.DetectVersion(ctx, run)
			if err != nil {
				t.Fatal(err)
			}
			if os.Getenv("GITGLANCE_PERF_OLD") != "" {
				// The view the old way (legacyView, golden_test.go), for comparison.
				run.reset()
				t0 := time.Now()
				snap, err := repo.Read(ctx, run, r.Dir)
				if err != nil {
					t.Fatal(err)
				}
				legacyView(t, run, ver, snap, rs, now)
				t.Logf("old path: total %v (%d git):%s", time.Since(t0).Round(time.Millisecond), run.n.Load(), run.breakdown())
			}
			cache := NewCache()
			for _, pass := range []string{"first view", "unchanged refresh"} {
				run.reset()
				t0 := time.Now()
				snap, err := repo.Read(ctx, run, r.Dir)
				if err != nil {
					t.Fatal(err)
				}
				readTime, readCalls := time.Since(t0), run.n.Load()
				t1 := time.Now()
				v, err := BuildCached(ctx, run, ver, snap, rs, now, cache)
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("%s: %d local, %d remote refs, %d sections; Read %v (%d git), Build %v (%d git), total %v",
					pass, len(snap.Local), len(snap.Remote), len(v.Sections), readTime.Round(time.Millisecond), readCalls,
					time.Since(t1).Round(time.Millisecond), run.n.Load()-readCalls, time.Since(t0).Round(time.Millisecond))
				t.Logf("%s git calls/time by subcommand:%s", pass, run.breakdown())
			}
		})
	}
}
