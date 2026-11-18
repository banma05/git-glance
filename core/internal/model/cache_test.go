package model

import (
	"context"
	"fmt"
	"strings"
	"testing"

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

// A refresh of an unchanged repository answers every branch from the cache:
// only the per-trunk counts run git again.
func TestUnchangedRefreshSpawnsAlmostNothing(t *testing.T) {
	r, rs := manyBranches(t, 12)
	ver, snap := readSnap(t, r)
	c := NewCache()
	run := &countingRunner{}
	now := r.Clock().Unix()
	if _, err := BuildCached(context.Background(), run, ver, snap, rs, now, c); err != nil {
		t.Fatal(err)
	}
	first := run.breakdown()
	if strings.Contains(first, "rev-parse=") {
		t.Errorf("first build resolved hashes the snapshot already has:%s", first)
	}
	run.reset()
	if _, err := BuildCached(context.Background(), run, ver, snap, rs, now, c); err != nil {
		t.Fatal(err)
	}
	if n := run.n.Load(); n > 3 {
		t.Fatalf("an unchanged refresh spawned %d git processes:%s", n, run.breakdown())
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
	f := newHashFacts(gitx.ExecRunner{}, snap, c)
	for _, ref := range snap.Local {
		if _, ok := c.get(f.unpushedKey(ref.Hash)); ok {
			t.Fatalf("an unpushed count of %s was kept", ref.Name)
		}
	}
}
