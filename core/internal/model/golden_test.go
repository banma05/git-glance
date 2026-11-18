package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/graph"
	"github.com/banma05/git-glance/core/internal/repo"
	"github.com/banma05/git-glance/core/internal/settings"
	"github.com/banma05/git-glance/core/internal/testrepo"
)

// legacyFacts answers a build's questions the way Build did before the
// cache: by full ref names resolved per call, one unpushed count per branch,
// nothing kept between builds.
type legacyFacts struct {
	m   *Matcher
	r   gitx.Runner
	dir string
}

func (l legacyFacts) ownCommits(ctx context.Context, t Target, ref repo.Ref) (ownResult, error) {
	own, fork, forkTime, err := l.m.OwnCommits(ctx, fullRef(t), headRef(ref.Name))
	if errors.Is(err, errNoForkPoint) {
		return ownResult{noFork: true}, nil
	}
	return ownResult{own: own, fork: fork, forkTime: forkTime}, err
}

func (l legacyFacts) contains(ctx context.Context, target Target, ref repo.Ref, o ownResult) (Containment, error) {
	return legacyContains(ctx, l.m, fullRef(target), headRef(ref.Name), o.fork, o.own)
}

func (l legacyFacts) unpushedAll(context.Context, []repo.Ref) map[string]int { return map[string]int{} }

func (l legacyFacts) unpushed(ctx context.Context, ref repo.Ref) (int, error) {
	return l.m.Unpushed(ctx, headRef(ref.Name))
}

func (l legacyFacts) between(ctx context.Context, newer, older string) (int, error) {
	return countBetween(ctx, l.r, l.dir, newer, older)
}

// legacyView builds the view the old way: one branch after the other, by ref
// names, and the graph in one pass asking git for each count as the layout
// needs it.
func legacyView(t *testing.T, r gitx.Runner, ver gitx.Version, snap repo.Snapshot, rs settings.RepoSettings, now int64) View {
	t.Helper()
	ctx := context.Background()
	facts := legacyFacts{m: &Matcher{R: r, Dir: snap.Repo}, r: r, dir: snap.Repo}
	v, err := build(ctx, NewCounter(r, snap.Repo, ver), facts, 1, snap, rs, now)
	if err != nil {
		t.Fatal(err)
	}
	trunks := uniqueTrunks(ResolveTrunks(snap, normalizeSettings(rs)))
	v.Sections = []graph.Section{}
	for i, g := range v.Groups {
		s, err := section(trunks[i], g.Branches, v.Branches, func(newer, older string) (int, error) {
			return countBetween(ctx, r, snap.Repo, newer, older)
		})
		if err != nil {
			t.Fatal(err)
		}
		v.Sections = append(v.Sections, s)
	}
	return v
}

func viewJSON(t *testing.T, v View) []byte {
	t.Helper()
	data, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// sameViews checks that the new build (cold, then from c) gives exactly the
// old view of the repository as it is now.
func sameViews(t *testing.T, r *testrepo.Repo, rs settings.RepoSettings, now int64, c *Cache) {
	t.Helper()
	ver, snap := readSnap(t, r)
	want := viewJSON(t, legacyView(t, gitx.ExecRunner{}, ver, snap, rs, now))
	for _, cache := range []*Cache{nil, c, c} {
		v, err := BuildCached(context.Background(), gitx.ExecRunner{}, ver, snap, rs, now, cache)
		if err != nil {
			t.Fatal(err)
		}
		if got := viewJSON(t, v); !bytes.Equal(got, want) {
			t.Fatalf("views differ (cache %v):\nold:\n%s\nnew:\n%s", cache != nil, want, got)
		}
	}
}

// kitchenSink has every kind of branch the matcher knows: squash-merged,
// merged with a merge commit, cherry-picked into the test branch, matched by
// subject only, many own commits, unrelated history, a same-named tag,
// partly pushed, upstream gone, in a dirty folder, and on a second trunk.
func kitchenSink(t *testing.T) (*testrepo.Repo, settings.RepoSettings) {
	t.Helper()
	r := testrepo.New(t)
	r.ApplyEnv()
	co := func(b string) { r.Git(r.Dir, "checkout", "-q", b) }
	branch := func(name, from string, commits int) {
		r.Git(r.Dir, "branch", "--no-track", name, from)
		co(name)
		for i := 0; i < commits; i++ {
			r.Commit(r.Dir, name+" work", map[string]string{name + "/" + string(rune('a'+i)) + ".txt": name + "\n"})
		}
		co("main")
	}
	for _, b := range []string{"v2", "main-test", "rel-1"} {
		r.Branch(b, "main")
	}
	r.Commit(r.Dir, "main one", map[string]string{"m1.txt": "m\n"})
	r.Push(r.Dir, "origin", "main", "v2", "main-test", "rel-1")

	branch("alice/squashed", "main", 2)
	r.Git(r.Dir, "merge", "--squash", "alice/squashed")
	r.Commit(r.Dir, "squashed work", nil)

	branch("alice/noff", "main", 2)
	r.Git(r.Dir, "merge", "--no-ff", "--no-commit", "alice/noff")
	r.Commit(r.Dir, "merge noff", nil)

	branch("alice/picked", "main", 1)
	co("main-test")
	r.Git(r.Dir, "cherry-pick", "--no-commit", "alice/picked")
	r.Commit(r.Dir, "picked", nil)
	r.Commit(r.Dir, "alice/subj work", map[string]string{"other.txt": "different\n"})
	co("main")
	branch("alice/subj", "main", 1)

	// One "fix" merged into the test branch, a second "fix" not.
	r.Git(r.Dir, "branch", "--no-track", "alice/halfway", "main")
	co("alice/halfway")
	r.Commit(r.Dir, "fix", map[string]string{"h1.txt": "1\n"})
	co("main-test")
	r.Git(r.Dir, "merge", "--no-ff", "--no-commit", "alice/halfway")
	r.Commit(r.Dir, "merge halfway", nil)
	co("alice/halfway")
	r.Commit(r.Dir, "fix", map[string]string{"h2.txt": "2\n"})
	co("main")

	branch("alice/long", "main", 5)
	wt := r.AddWorktree("long", "alice/long")
	r.Commit(r.Dir, "main two", map[string]string{"m2.txt": "m\n"})
	if err := writeFile(wt+"/dirty.txt", "x\n"); err != nil {
		t.Fatal(err)
	}

	co("main")
	r.Git(r.Dir, "checkout", "-q", "--orphan", "alice/orphan")
	r.Commit(r.Dir, "unrelated", map[string]string{"o.txt": "o\n"})
	co("main")

	branch("v9", "main", 1)
	r.Git(r.Dir, "tag", "v9", "main~1")

	branch("alice/partial", "main", 1)
	r.Push(r.Dir, "origin", "alice/partial")
	co("alice/partial")
	r.Commit(r.Dir, "not pushed", map[string]string{"np.txt": "n\n"})
	co("main")

	branch("alice/gone", "main", 1)
	r.Push(r.Dir, "-u", "origin", "alice/gone")
	r.Git(r.Dir, "push", "origin", "--delete", "alice/gone")

	branch("alice/onv2", "v2", 2)
	r.Push(r.Dir, "origin", "main", "main-test")
	r.Git(r.Dir, "fetch", "-q", "--prune", "origin")
	rs := settings.RepoSettings{
		Trunks:     []settings.TrunkConfig{{Name: "main", Test: "main-test", Releases: []string{"rel-1"}}, {Name: "v2"}},
		HidePrefix: "alice/",
	}
	return r, rs
}

// The cached, parallel, hash-based build gives byte-identical views to the
// old sequential build by ref names, cold and warm, and after a change.
func TestBuildMatchesTheOldPath(t *testing.T) {
	t.Run("kitchen sink", func(t *testing.T) {
		r, rs := kitchenSink(t)
		c := NewCache()
		now := r.Clock().Unix() + 3600
		sameViews(t, r, rs, now, c)
		// Moves: a branch gains a commit, the test branch picks another,
		// a branch is pushed. The warm cache must follow.
		r.Git(r.Dir, "checkout", "-q", "alice/subj")
		r.Commit(r.Dir, "subj two", map[string]string{"s2.txt": "s\n"})
		r.Git(r.Dir, "checkout", "-q", "main-test")
		r.Git(r.Dir, "cherry-pick", "--no-commit", "alice/onv2")
		r.Commit(r.Dir, "picked v2", nil)
		r.Git(r.Dir, "checkout", "-q", "main")
		r.Push(r.Dir, "origin", "alice/partial", "main-test")
		r.Git(r.Dir, "fetch", "-q", "origin")
		sameViews(t, r, rs, r.Clock().Unix()+3600, c)
	})
	t.Run("many branches", func(t *testing.T) {
		r, rs := manyBranches(t, 12)
		sameViews(t, r, rs, r.Clock().Unix(), NewCache())
	})
	t.Run("synthetic", func(t *testing.T) {
		r, rs, now := perfRepo(t, perfShape{main: 400, v2: 60, features: 40, remotes: 300})
		sameViews(t, r, rs, now, NewCache())
		// The same repository seen long after: every branch is old.
		sameViews(t, r, rs, now+400*86400, NewCache())
	})
}

func writeFile(path, content string) error { return os.WriteFile(path, []byte(content), 0o644) }

// legacyContains is Contains as it was before the shared target-side walk
// and the reordered squash step, kept here to prove the results unchanged:
// refs are resolved per call, each step runs its own git command.
func legacyContains(ctx context.Context, m *Matcher, targetRef, branchRef, fork string, own []repo.CommitInfo) (Containment, error) {
	resolve := func(ref string) (string, error) {
		out, err := m.run(ctx, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
		return strings.TrimSpace(out), err
	}
	target, err := resolve(targetRef)
	if err != nil {
		return Containment{}, err
	}
	branch, err := resolve(branchRef)
	if err != nil {
		return Containment{}, err
	}
	if err := checkFresh(branch, fork, own); err != nil {
		return Containment{}, err
	}
	per := make([]CommitMatch, len(own))
	for i, c := range own {
		per[i] = CommitMatch{CommitInfo: c}
	}
	steps := []func(context.Context, *Matcher, string, string, string, []CommitMatch) error{
		legacyAncestor, legacyPatch, legacySubject, legacySquash,
	}
	for _, s := range steps {
		if len(unmatched(per)) == 0 {
			break
		}
		if err := s(ctx, m, target, branch, fork, per); err != nil {
			return Containment{}, err
		}
	}
	return summarize(per), nil
}

func legacyAncestor(ctx context.Context, m *Matcher, target, _, fork string, per []CommitMatch) error {
	out, err := m.run(ctx, "rev-list", fork+".."+target, "--")
	if err != nil {
		return err
	}
	inTarget := map[string]bool{}
	for _, h := range strings.Fields(out) {
		inTarget[h] = true
	}
	for _, i := range unmatched(per) {
		if inTarget[per[i].Hash] {
			per[i].By = byAncestor
		}
	}
	return nil
}

func legacyPatch(ctx context.Context, m *Matcher, target, branch, fork string, per []CommitMatch) error {
	out, err := m.run(ctx, "cherry", target, branch, fork)
	if err != nil {
		return err
	}
	equivalent := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") {
			equivalent[strings.TrimSpace(line[2:])] = true
		}
	}
	for _, i := range unmatched(per) {
		if equivalent[per[i].Hash] {
			per[i].By = byPatch
		}
	}
	return nil
}

func legacySubject(ctx context.Context, m *Matcher, target, branch, _ string, per []CommitMatch) error {
	out, err := m.run(ctx, "log", noSignature, "--format=%s", branch+".."+target, "--")
	if err != nil {
		return err
	}
	left := map[string]int{}
	for _, line := range strings.Split(out, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			left[s]++
		}
	}
	for _, p := range per {
		if s := strings.TrimSpace(p.Subject); p.By == byPatch && left[s] > 0 {
			left[s]--
		}
	}
	for _, i := range unmatched(per) {
		if s := strings.TrimSpace(per[i].Subject); s != "" && left[s] > 0 {
			per[i].By = bySubject
			left[s]--
		}
	}
	return nil
}

func legacySquash(ctx context.Context, m *Matcher, target, branch, fork string, per []CommitMatch) error {
	names, err := m.run(ctx, "diff", "--name-only", "-z", "--no-ext-diff", "--no-color", "--no-textconv", fork, branch, "--")
	if err != nil {
		return err
	}
	diff, err := m.run(ctx, "diff", "--no-ext-diff", "--no-color", "--no-textconv", fork, branch, "--")
	if err != nil || strings.TrimSpace(diff) == "" {
		return err
	}
	whole, err := m.patchIDs(ctx, diff)
	if err != nil || len(whole) != 1 {
		return err
	}
	args := []string{"log", noSignature, "-p", "--full-diff", "--format=%H", "--no-ext-diff", "--no-color", "--no-textconv",
		"-n", strconv.Itoa(maxSquashCands), branch + ".." + target, "--"}
	files := strings.Split(strings.Trim(names, "\x00"), "\x00")
	if len(files) > maxPathspecFiles {
		files = files[:maxPathspecFiles]
	}
	for _, f := range files {
		args = append(args, literalPathPrefix+f)
	}
	patches, err := m.run(ctx, args...)
	if err != nil || strings.TrimSpace(patches) == "" {
		return err
	}
	ids, err := m.patchIDs(ctx, patches)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if id == whole[0] {
			for _, i := range unmatched(per) {
				per[i].By = bySquash
			}
			return nil
		}
	}
	return nil
}
