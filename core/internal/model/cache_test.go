package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/repo"
	"github.com/banma05/git-glance/core/internal/settings"
	"github.com/banma05/git-glance/core/internal/testrepo"
)

// manyBranches builds main and v2 with n feature branches on them, half
// pushed, one cherry-picked into main, and returns the repo and settings.
func manyBranches(t *testing.T, n int) (*testrepo.Repo, settings.RepoSettings) {
	t.Helper()
	r := testrepo.New(t)
	r.ApplyEnv()
	r.Branch("v2", "main")
	r.Commit(r.Dir, "main one", map[string]string{"m.txt": "m\n"})
	r.Git(r.Dir, "checkout", "-q", "v2")
	r.Commit(r.Dir, "v2 one", map[string]string{"v.txt": "v\n"})
	r.Git(r.Dir, "checkout", "-q", "main")
	r.Push(r.Dir, "origin", "main", "v2")
	for i := 0; i < n; i++ {
		b := fmt.Sprintf("alice/f%02d", i)
		base := []string{"main", "v2"}[i%2]
		r.Git(r.Dir, "branch", "--no-track", b, "origin/"+base)
		r.Git(r.Dir, "checkout", "-q", b)
		r.Commit(r.Dir, "work "+b, map[string]string{b + ".txt": b + "\n"})
		if i%3 == 0 {
			r.Commit(r.Dir, "more "+b, map[string]string{b + "-2.txt": b + "\n"})
		}
		r.Git(r.Dir, "checkout", "-q", "main")
		if i%2 == 0 {
			r.Push(r.Dir, "origin", b)
		}
	}
	r.Git(r.Dir, "cherry-pick", r.Git(r.Dir, "rev-parse", "alice/f02"))
	r.Push(r.Dir, "origin", "main")
	r.Git(r.Dir, "fetch", "-q", "origin")
	return r, settings.RepoSettings{Trunks: []settings.TrunkConfig{{Name: "main"}, {Name: "v2"}}}
}

func readSnap(t *testing.T, r *testrepo.Repo) (gitx.Version, repo.Snapshot) {
	t.Helper()
	ver, err := gitx.DetectVersion(context.Background(), gitx.ExecRunner{})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := repo.Read(context.Background(), gitx.ExecRunner{}, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	return ver, snap
}

// A refresh of an unchanged repository answers every branch from the cache,
// on git without %(ahead-behind:) (2.40) as well as with it: only the
// per-repository commands run git again (the history fingerprint: rev-parse
// --git-common-dir and for-each-ref refs/replace; on 2.41+ one for-each-ref
// per trunk).
func TestUnchangedRefreshSpawnsAlmostNothing(t *testing.T) {
	r, rs := manyBranches(t, 12)
	installed, snap := readSnap(t, r)
	for _, ver := range []gitx.Version{installed, {Major: 2, Minor: 40}} {
		c := NewCache()
		run := &countingRunner{}
		now := r.Clock().Unix()
		if _, err := BuildCached(context.Background(), run, ver, snap, rs, now, c); err != nil {
			t.Fatal(err)
		}
		if n := run.count("rev-parse --verify"); n > 0 {
			t.Errorf("%v: first build resolved %d hashes the snapshot already has:%s", ver, n, run.breakdown())
		}
		run.reset()
		if _, err := BuildCached(context.Background(), run, ver, snap, rs, now, c); err != nil {
			t.Fatal(err)
		}
		if n := run.n.Load(); n > 4 {
			t.Fatalf("%v: an unchanged refresh spawned %d git processes:%s", ver, n, run.breakdown())
		}
	}
}

func TestCacheKeepsTheMostRecentlyUsed(t *testing.T) {
	c := newCacheSize(2)
	c.put("a", 1)
	c.put("b", 2)
	c.get("a")
	c.put("c", 3)
	if _, ok := c.get("b"); ok {
		t.Fatal("the least recently used entry was kept")
	}
	for k, want := range map[string]int{"a": 1, "c": 3} {
		if v, ok := c.get(k); !ok || v.(int) != want {
			t.Fatalf("%s = %v, %v", k, v, ok)
		}
	}
	var none *Cache
	none.put("a", 1)
	if _, ok := none.get("a"); ok {
		t.Fatal("a nil cache kept something")
	}
}

// One walk counts every branch exactly as `rev-list --count <b> --not
// --remotes` does: merged-in unpushed work, shared commits, a branch at or
// behind a remote branch, and a branch whose history no remote has.
func TestCountUnpushedMatchesPerBranch(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	co := func(b string) { r.Git(r.Dir, "checkout", "-q", b) }
	r.Git(r.Dir, "checkout", "-q", "-b", "a")
	r.Commit(r.Dir, "a1", map[string]string{"a1": "x"})
	r.Commit(r.Dir, "a2", map[string]string{"a2": "x"})
	r.Git(r.Dir, "checkout", "-q", "-b", "b")
	r.Commit(r.Dir, "b1", map[string]string{"b1": "x"})
	co("main")
	r.Git(r.Dir, "checkout", "-q", "-b", "c")
	r.Commit(r.Dir, "c1", map[string]string{"c1": "x"})
	r.Git(r.Dir, "merge", "-q", "--no-ff", "-m", "merge b", "b")
	r.Push(r.Dir, "origin", "a~1:refs/heads/a-half")
	co("main")
	r.Git(r.Dir, "branch", "pushed", "main")
	r.Git(r.Dir, "branch", "behind", "main")
	r.Commit(r.Dir, "main moves", map[string]string{"m": "x"})
	r.Push(r.Dir, "origin", "main")
	r.Git(r.Dir, "checkout", "-q", "--orphan", "lonely")
	r.Commit(r.Dir, "lonely", map[string]string{"l": "x"})
	co("main")
	r.Git(r.Dir, "fetch", "-q", "origin")
	names := []string{"a", "b", "c", "pushed", "behind", "lonely", "main"}
	var tips []string
	for _, n := range names {
		tips = append(tips, r.Git(r.Dir, "rev-parse", "refs/heads/"+n))
	}
	got, err := countUnpushed(context.Background(), gitx.ExecRunner{}, r.Dir, tips)
	if err != nil {
		t.Fatal(err)
	}
	m := &Matcher{R: gitx.ExecRunner{}, Dir: r.Dir}
	for i, n := range names {
		want, err := m.Unpushed(context.Background(), "refs/heads/"+n)
		if err != nil {
			t.Fatal(err)
		}
		if got[tips[i]] != want {
			t.Errorf("%s: batched %d, per branch %d", n, got[tips[i]], want)
		}
	}
	if got[tips[2]] < 3 {
		t.Fatalf("c should count its merged-in work: %v", got)
	}
}

// remoteMover moves a remote-tracking ref right before the batched count.
type remoteMover struct {
	gitx.ExecRunner
	r    *testrepo.Repo
	done bool
}

func (m *remoteMover) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if !m.done && len(args) > 1 && args[0] == "rev-list" && args[1] == "--parents" {
		m.done = true
		m.r.Git(m.r.Dir, "update-ref", "refs/remotes/origin/moved", "main")
	}
	return m.ExecRunner.Run(ctx, dir, args...)
}

// Counts made while the remote-tracking refs differ from the snapshot's are
// used for that view but not kept under the snapshot's fingerprint.
func TestUnpushedNotCachedWhenRemotesMove(t *testing.T) {
	r, rs := manyBranches(t, 4)
	ver, snap := readSnap(t, r)
	c := NewCache()
	if _, err := BuildCached(context.Background(), &remoteMover{r: r}, ver, snap, rs, r.Clock().Unix(), c); err != nil {
		t.Fatal(err)
	}
	f := newHashFacts(context.Background(), gitx.ExecRunner{}, snap, c)
	for _, ref := range snap.Local {
		if _, ok := f.get(f.unpushedKey(ref.Hash)); ok {
			t.Fatalf("an unpushed count of %s was kept", ref.Name)
		}
	}
}

// On git without %(ahead-behind:) one walk of the history counts every branch
// against every trunk.
func TestOldGitCountsInOneWalk(t *testing.T) {
	r, rs := manyBranches(t, 12)
	_, snap := readSnap(t, r)
	run := &countingRunner{}
	if _, err := BuildCached(context.Background(), run, gitx.Version{Major: 2, Minor: 40}, snap, rs, r.Clock().Unix(), NewCache()); err != nil {
		t.Fatal(err)
	}
	if n := run.count("rev-list --left-right"); n > 0 {
		t.Fatalf("%d per-branch counts ran:%s", n, run.breakdown())
	}
}

// The one-walk counts equal `rev-list --left-right --count trunk...tip` for
// merged-in work, a branch behind, ahead, both, and unrelated history.
func TestCountAheadBehindMatchesRevList(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	co := func(b string) { r.Git(r.Dir, "checkout", "-q", b) }
	r.Git(r.Dir, "checkout", "-q", "-b", "a")
	r.Commit(r.Dir, "a1", map[string]string{"a1": "x"})
	r.Git(r.Dir, "checkout", "-q", "-b", "b")
	r.Commit(r.Dir, "b1", map[string]string{"b1": "x"})
	co("main")
	r.Git(r.Dir, "branch", "behind", "main")
	r.Commit(r.Dir, "m1", map[string]string{"m1": "x"})
	r.Git(r.Dir, "merge", "-q", "--no-ff", "-m", "merge a", "a")
	r.Git(r.Dir, "checkout", "-q", "-b", "c", "a")
	r.Git(r.Dir, "merge", "-q", "--no-ff", "-m", "merge main", "main")
	r.Commit(r.Dir, "c1", map[string]string{"c1": "x"})
	r.Git(r.Dir, "checkout", "-q", "--orphan", "lonely")
	r.Commit(r.Dir, "lonely", map[string]string{"l": "x"})
	co("main")
	trunk := r.Git(r.Dir, "rev-parse", "main")
	var pairs []tipPair
	names := []string{"a", "b", "c", "behind", "lonely", "main"}
	for _, n := range names {
		pairs = append(pairs, tipPair{trunk, r.Git(r.Dir, "rev-parse", "refs/heads/"+n)})
	}
	got, err := countAheadBehind(context.Background(), gitx.ExecRunner{}, r.Dir, pairs)
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range pairs {
		want, err := parseCounts(r.Git(r.Dir, "rev-list", "--left-right", "--count", p.trunk+"..."+p.tip, "--"), true)
		if err != nil {
			t.Fatal(err)
		}
		if got[p] != want {
			t.Errorf("%s: walk %+v, rev-list %+v", names[i], got[p], want)
		}
	}
}

// viewOf builds the view of dir with c (nil: uncached) as JSON.
func viewOf(t *testing.T, dir string, rs settings.RepoSettings, now int64, c *Cache) string {
	t.Helper()
	ver, err := gitx.DetectVersion(context.Background(), gitx.ExecRunner{})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := repo.Read(context.Background(), gitx.ExecRunner{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	v, err := BuildCached(context.Background(), gitx.ExecRunner{}, ver, snap, rs, now, c)
	if err != nil {
		t.Fatal(err)
	}
	return string(viewJSON(t, v))
}

// The same tips can stand for another history: a shallow clone deepened,
// or a replace ref added. Cached answers must not survive that.
func TestCacheFollowsShallowAndReplaceChanges(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	r.Branch("feature", "main")
	r.Git(r.Dir, "checkout", "-q", "feature")
	r.Commit(r.Dir, "f1", map[string]string{"f1": "x"})
	r.Git(r.Dir, "checkout", "-q", "main")
	r.Commit(r.Dir, "m1", map[string]string{"m1": "x"})
	r.Push(r.Dir, "origin", "main", "feature")
	url := "file://" + r.Remote
	if !strings.HasPrefix(r.Remote, "/") {
		url = "file:///" + r.Remote
	}
	clone := r.Root + "/shallow"
	r.Git(r.Root, "clone", "-q", "--depth", "1", "--no-single-branch", url, clone)
	r.Git(clone, "branch", "feature", "origin/feature")
	rs := settings.RepoSettings{Trunks: []settings.TrunkConfig{{Name: "main"}}}
	now := r.Clock().Unix() + 3600
	c := NewCache()
	shallow := viewOf(t, clone, rs, now, c)
	r.Git(clone, "fetch", "-q", "--unshallow")
	if fresh := viewOf(t, clone, rs, now, nil); fresh == shallow {
		t.Fatal("the deepened clone should look different")
	} else if cached := viewOf(t, clone, rs, now, c); cached != fresh {
		t.Fatalf("after --unshallow the cache answered from the shallow history:\n%s\nwant\n%s", cached, fresh)
	}
	before := viewOf(t, clone, rs, now, c)
	r.Git(clone, "replace", "--graft", r.Git(clone, "rev-parse", "feature"), r.Git(clone, "rev-parse", "main"))
	if fresh := viewOf(t, clone, rs, now, nil); fresh == before {
		t.Fatal("the graft should change the view")
	} else if cached := viewOf(t, clone, rs, now, c); cached != fresh {
		t.Fatalf("after a replace ref the cache answered from the old history:\n%s\nwant\n%s", cached, fresh)
	}
}

// inFlightRunner records the most per-branch git commands running at once.
type inFlightRunner struct {
	gitx.ExecRunner
	mu       sync.Mutex
	now, max int
}

func (f *inFlightRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	switch args[0] {
	case "merge-base", "cherry", "log", "diff":
		f.mu.Lock()
		f.now++
		f.max = max(f.max, f.now)
		f.mu.Unlock()
		defer func() { f.mu.Lock(); f.now--; f.mu.Unlock() }()
		time.Sleep(20 * time.Millisecond)
	}
	return f.ExecRunner.Run(ctx, dir, args...)
}

// Concurrent builds share one limit of buildWorkers git processes for their
// per-branch work.
func TestConcurrentBuildsShareTheWorkerLimit(t *testing.T) {
	r, rs := manyBranches(t, 12)
	ver, snap := readSnap(t, r)
	run := &inFlightRunner{}
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := BuildCached(context.Background(), run, ver, snap, rs, r.Clock().Unix(), nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if run.max > buildWorkers {
		t.Fatalf("%d per-branch git commands ran at once, the limit is %d", run.max, buildWorkers)
	}
}

// forEach still runs every call with no workers asked for, and a call that
// waits for a slot gives up when its context ends.
func TestForEachGuards(t *testing.T) {
	ran := make([]bool, 3)
	done := make(chan []error)
	go func() { done <- forEach(context.Background(), 3, 0, func(i int) error { ran[i] = true; return nil }) }()
	select {
	case errs := <-done:
		for i := range ran {
			if !ran[i] || errs[i] != nil {
				t.Fatalf("call %d: ran %v, error %v", i, ran[i], errs[i])
			}
		}
	case <-time.After(10 * time.Second):
		t.Fatal("forEach with 0 workers never returned")
	}
	for i := 0; i < buildWorkers; i++ {
		buildSlots <- struct{}{}
	}
	defer func() {
		for i := 0; i < buildWorkers; i++ {
			<-buildSlots
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	errs := forEach(ctx, 2, 2, func(int) error { t.Error("ran without a slot"); return nil })
	for _, err := range errs {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiting call returned %v, want context.Canceled", err)
		}
	}
}
