package model

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/repo"
	"github.com/banma05/git-glance/core/internal/testrepo"
)

// containFixture builds feature/a with n commits off main and returns the
// repo plus the own commits and fork computed before anything is merged.
func containFixture(t *testing.T, n int) (*testrepo.Repo, *Matcher, []repo.CommitInfo, string) {
	t.Helper()
	r := testrepo.New(t)
	r.ApplyEnv()
	r.Branch("feature/a", "main")
	r.Branch("main-test", "main")
	r.Git(r.Dir, "switch", "feature/a")
	for i := 0; i < n; i++ {
		name := string(rune('a'+i)) + ".txt"
		r.Commit(r.Dir, "add "+name, map[string]string{name: "content " + name + "\n"})
	}
	m := &Matcher{R: gitx.ExecRunner{}, Dir: r.Dir}
	own, fork, _, err := m.OwnCommits(context.Background(), "main", "feature/a")
	if err != nil {
		t.Fatal(err)
	}
	if len(own) != n {
		t.Fatalf("own = %d want %d", len(own), n)
	}
	return r, m, own, fork
}

func bys(c Containment) []string {
	var out []string
	for _, p := range c.Per {
		out = append(out, p.By)
	}
	return out
}

func TestOwnCommits(t *testing.T) {
	r, m, own, fork := containFixture(t, 2)
	if own[0].Subject != "add b.txt" || own[1].Subject != "add a.txt" {
		t.Fatalf("order: %+v", own)
	}
	if fork != r.Git(r.Dir, "rev-parse", "main") {
		t.Fatalf("fork %s", fork)
	}
	if _, _, _, err := m.OwnCommits(context.Background(), "main", "does-not-exist"); err == nil {
		t.Fatal("want error for unknown ref")
	}
}

func TestContainsEmptyOwn(t *testing.T) {
	_, m, own, fork := containFixture(t, 0)
	c, err := m.containsRefs(context.Background(), "main", "feature/a", fork, own)
	if err != nil {
		t.Fatal(err)
	}
	if c.State != "all" || c.Total != 0 || c.Per == nil || len(c.Per) != 0 {
		t.Fatalf("%+v", c)
	}
}

func TestContainsAncestor(t *testing.T) {
	r, m, own, fork := containFixture(t, 2)
	r.Git(r.Dir, "switch", "main")
	r.Git(r.Dir, "merge", "--no-ff", "-m", "merge feature/a", "feature/a")
	c, err := m.containsRefs(context.Background(), "main", "feature/a", fork, own)
	if err != nil {
		t.Fatal(err)
	}
	if c.State != "all" || c.Matched != 2 || c.Total != 2 {
		t.Fatalf("%+v", c)
	}
	for _, p := range c.Per {
		if p.By != "ancestor" {
			t.Fatalf("by %q", p.By)
		}
	}
	if c.Per[0].Hash != own[0].Hash || c.Per[1].Hash != own[1].Hash {
		t.Fatalf("order not kept: %+v", c.Per)
	}
}

func TestContainsPatch(t *testing.T) {
	r, m, own, fork := containFixture(t, 1)
	r.Git(r.Dir, "switch", "main-test")
	r.Git(r.Dir, "cherry-pick", own[0].Hash)
	c, err := m.containsRefs(context.Background(), "main-test", "feature/a", fork, own)
	if err != nil {
		t.Fatal(err)
	}
	if c.State != "all" || c.Per[0].By != "patch" {
		t.Fatalf("%+v", c)
	}
}

func TestContainsSubject(t *testing.T) {
	r, m, own, fork := containFixture(t, 1)
	r.Git(r.Dir, "switch", "main-test")
	r.Commit(r.Dir, "  "+own[0].Subject+" ", map[string]string{"other.txt": "different\n"})
	c, err := m.containsRefs(context.Background(), "main-test", "feature/a", fork, own)
	if err != nil {
		t.Fatal(err)
	}
	if c.State != "all" || c.Per[0].By != "subject" {
		t.Fatalf("%+v", c)
	}
}

func TestContainsSubjectIgnoresBranchItself(t *testing.T) {
	// A target that does not contain the commit must not match on the
	// branch's own subjects.
	_, m, own, fork := containFixture(t, 1)
	c, err := m.containsRefs(context.Background(), "main-test", "feature/a", fork, own)
	if err != nil {
		t.Fatal(err)
	}
	if c.State != "none" {
		t.Fatalf("%+v", c)
	}
}

func TestContainsSquash(t *testing.T) {
	r, m, own, fork := containFixture(t, 3)
	r.Git(r.Dir, "switch", "main")
	r.Commit(r.Dir, "squashed work", map[string]string{
		"a.txt": "content a.txt\n", "b.txt": "content b.txt\n", "c.txt": "content c.txt\n",
	})
	c, err := m.containsRefs(context.Background(), "main", "feature/a", fork, own)
	if err != nil {
		t.Fatal(err)
	}
	if c.State != "all" || c.Matched != 3 {
		t.Fatalf("%+v", c)
	}
	for _, by := range bys(c) {
		if by != "squash" {
			t.Fatalf("by %v", bys(c))
		}
	}
}

func TestContainsSquashNeedsExactContent(t *testing.T) {
	r, m, own, fork := containFixture(t, 2)
	r.Git(r.Dir, "switch", "main")
	r.Commit(r.Dir, "almost", map[string]string{"a.txt": "content a.txt\n", "b.txt": "changed\n"})
	c, err := m.containsRefs(context.Background(), "main", "feature/a", fork, own)
	if err != nil {
		t.Fatal(err)
	}
	if c.State != "none" {
		t.Fatalf("%+v", c)
	}
}

func TestContainsSome(t *testing.T) {
	r, m, own, fork := containFixture(t, 2)
	r.Git(r.Dir, "switch", "main-test")
	r.Git(r.Dir, "cherry-pick", own[1].Hash)
	c, err := m.containsRefs(context.Background(), "main-test", "feature/a", fork, own)
	if err != nil {
		t.Fatal(err)
	}
	if c.State != "some" || c.Matched != 1 || c.Total != 2 {
		t.Fatalf("%+v", c)
	}
	if c.Per[0].By != "" || c.Per[1].By != "patch" {
		t.Fatalf("%v", bys(c))
	}
	again, err := m.containsRefs(context.Background(), "main-test", "feature/a", fork, own)
	if err != nil || again.State != "some" {
		t.Fatalf("cached: %+v %v", again, err)
	}
}

func TestContainsNone(t *testing.T) {
	_, m, own, fork := containFixture(t, 2)
	c, err := m.containsRefs(context.Background(), "main-test", "feature/a", fork, own)
	if err != nil {
		t.Fatal(err)
	}
	if c.State != "none" || c.Matched != 0 || c.Total != 2 || len(c.Per) != 2 {
		t.Fatalf("%+v", c)
	}
}

func TestUnpushed(t *testing.T) {
	r, m, own, _ := containFixture(t, 2)
	r.Push(r.Dir, "origin", own[1].Hash+":refs/heads/feature/a")
	n, err := m.Unpushed(context.Background(), "feature/a")
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	r.Push(r.Dir, "origin", own[0].Hash+":refs/heads/other")
	n, err = m.Unpushed(context.Background(), "feature/a")
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if strings.TrimSpace(r.Git(r.Dir, "rev-parse", "feature/a")) != own[0].Hash {
		t.Fatal("fixture changed")
	}
}

func TestContainsSubjectNotVouchedByMergedOwn(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	r.Branch("feature/a", "main")
	r.Branch("main-test", "main")
	r.Git(r.Dir, "switch", "feature/a")
	r.Commit(r.Dir, "fix", map[string]string{"a.txt": "a\n"})
	r.Git(r.Dir, "switch", "main-test")
	r.Git(r.Dir, "merge", "--no-ff", "-m", "merge a", "feature/a")
	r.Git(r.Dir, "switch", "feature/a")
	r.Commit(r.Dir, "fix", map[string]string{"b.txt": "b\n"})
	m := &Matcher{R: gitx.ExecRunner{}, Dir: r.Dir}
	own, fork, _, err := m.OwnCommits(context.Background(), "main", "feature/a")
	if err != nil {
		t.Fatal(err)
	}
	c, err := m.containsRefs(context.Background(), "main-test", "feature/a", fork, own)
	if err != nil {
		t.Fatal(err)
	}
	if c.State != "some" || c.Per[0].By != "" || c.Per[1].By != "ancestor" {
		t.Fatalf("%+v", c)
	}
}

func TestContainsRejectsStaleOwn(t *testing.T) {
	r, m, own, fork := containFixture(t, 1)
	r.Git(r.Dir, "commit", "--amend", "-m", "reworded")
	if _, err := m.containsRefs(context.Background(), "main", "feature/a", fork, own); err == nil {
		t.Fatal("want error for stale own")
	}
	if _, err := m.containsRefs(context.Background(), "main", "feature/a", fork, nil); err == nil {
		t.Fatal("want error for empty own on a branch with commits")
	}
}

func TestContainsCacheMissWhenTargetMoves(t *testing.T) {
	r, m, own, fork := containFixture(t, 1)
	ctx := context.Background()
	c, err := m.containsRefs(ctx, "main-test", "feature/a", fork, own)
	if err != nil || c.State != "none" {
		t.Fatalf("%+v %v", c, err)
	}
	r.Git(r.Dir, "switch", "main-test")
	r.Git(r.Dir, "cherry-pick", own[0].Hash)
	c, err = m.containsRefs(ctx, "main-test", "feature/a", fork, own)
	if err != nil || c.State != "all" || c.Per[0].By != "ancestor" && c.Per[0].By != "patch" {
		t.Fatalf("%+v %v", c, err)
	}
}

// squashSetup makes a branch of two commits off main (main may first receive
// base files) and returns the matcher, own commits and fork.
func squashSetup(t *testing.T, base map[string]string, first, second map[string]string, removeFirst string) (*testrepo.Repo, *Matcher, []repo.CommitInfo, string) {
	t.Helper()
	r := testrepo.New(t)
	r.ApplyEnv()
	if base != nil {
		r.Commit(r.Dir, "base files", base)
	}
	r.Branch("feature/a", "main")
	r.Git(r.Dir, "switch", "feature/a")
	if removeFirst != "" {
		r.Git(r.Dir, "mv", removeFirst, "new.txt")
	}
	r.Commit(r.Dir, "first part", first)
	r.Commit(r.Dir, "second part", second)
	m := &Matcher{R: gitx.ExecRunner{}, Dir: r.Dir}
	own, fork, _, err := m.OwnCommits(context.Background(), "main", "feature/a")
	if err != nil {
		t.Fatal(err)
	}
	r.Git(r.Dir, "switch", "main")
	return r, m, own, fork
}

func TestContainsSquashManyFiles(t *testing.T) {
	first, second, all := map[string]string{}, map[string]string{}, map[string]string{}
	for i := 0; i < 101; i++ {
		name := fmt.Sprintf("dir/f%03d.txt", i)
		all[name] = name + "\n"
		if i < 51 {
			first[name] = name + "\n"
		} else {
			second[name] = name + "\n"
		}
	}
	r, m, own, fork := squashSetup(t, nil, first, second, "")
	r.Commit(r.Dir, "everything at once", all)
	c, err := m.containsRefs(context.Background(), "main", "feature/a", fork, own)
	if err != nil {
		t.Fatal(err)
	}
	if c.State != "all" || bys(c)[0] != "squash" || bys(c)[1] != "squash" {
		t.Fatalf("%+v", c)
	}
}

func TestContainsSquashRename(t *testing.T) {
	base := map[string]string{"old.txt": "a long enough body\nsecond line\nthird line\nfourth line\n", "z.txt": "z\n"}
	r, m, own, fork := squashSetup(t, base, map[string]string{"z.txt": "z1\n"}, map[string]string{"z.txt": "z2\n"}, "old.txt")
	r.Git(r.Dir, "mv", "old.txt", "new.txt")
	r.Commit(r.Dir, "rename and edit", map[string]string{"z.txt": "z2\n"})
	c, err := m.containsRefs(context.Background(), "main", "feature/a", fork, own)
	if err != nil {
		t.Fatal(err)
	}
	if c.State != "all" || bys(c)[0] != "squash" {
		t.Fatalf("%+v", c)
	}
}

func TestContainsSquashRejectsExtraFile(t *testing.T) {
	r, m, own, fork := squashSetup(t, nil, map[string]string{"a.txt": "a\n"}, map[string]string{"b.txt": "b\n"}, "")
	r.Commit(r.Dir, "more than the branch", map[string]string{"a.txt": "a\n", "b.txt": "b\n", "extra.txt": "x\n"})
	c, err := m.containsRefs(context.Background(), "main", "feature/a", fork, own)
	if err != nil {
		t.Fatal(err)
	}
	if c.State != "none" {
		t.Fatalf("%+v", c)
	}
}

// subjectFixture gives feature/a two own commits that share one subject.
func subjectFixture(t *testing.T) (*testrepo.Repo, *Matcher) {
	t.Helper()
	r := testrepo.New(t)
	r.ApplyEnv()
	r.Branch("feature/a", "main")
	r.Git(r.Dir, "switch", "feature/a")
	r.Commit(r.Dir, "fix", map[string]string{"a.txt": "a\n"})
	r.Commit(r.Dir, "fix", map[string]string{"b.txt": "b\n"})
	r.Git(r.Dir, "switch", "main")
	return r, &Matcher{R: gitx.ExecRunner{}, Dir: r.Dir}
}

func containsOnMain(t *testing.T, m *Matcher) Containment {
	t.Helper()
	own, fork, _, err := m.OwnCommits(context.Background(), "main", "feature/a")
	if err != nil {
		t.Fatal(err)
	}
	c, err := m.containsRefs(context.Background(), "main", "feature/a", fork, own)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Each target commit vouches for at most one own commit with its subject: one
// "fix" on the trunk does not make two own "fix" commits merged.
func TestContainsSubjectCountsEachTargetCommitOnce(t *testing.T) {
	r, m := subjectFixture(t)
	r.Commit(r.Dir, "fix", map[string]string{"c.txt": "c\n"})
	if c := containsOnMain(t, m); c.State != "some" || c.Matched != 1 {
		t.Fatalf("%+v", c)
	}
	r.Commit(r.Dir, "fix", map[string]string{"d.txt": "d\n"})
	if c := containsOnMain(t, m); c.State != "all" || c.Matched != 2 {
		t.Fatalf("two trunk commits: %+v", c)
	}
}

// A trunk commit that already matched an own commit by its patch has vouched:
// it cannot vouch again for another own commit by the same subject.
func TestContainsSubjectNotVouchedTwiceAfterPatch(t *testing.T) {
	r, m := subjectFixture(t)
	first := r.Git(r.Dir, "rev-parse", "feature/a~1")
	r.Git(r.Dir, "cherry-pick", first)
	c := containsOnMain(t, m)
	if c.State != "some" || c.Matched != 1 || c.Per[1].By != "patch" || c.Per[0].By != "" {
		t.Fatalf("%+v", c)
	}
}

// containsRefs is Contains for ref names: it resolves both to commit hashes
// first, as callers outside a build do.
func (m *Matcher) containsRefs(ctx context.Context, targetRef, branchRef, fork string, own []repo.CommitInfo) (Containment, error) {
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
	return m.Contains(ctx, target, branch, fork, own)
}

// An unmerged branch whose files no trunk commit touches is settled with few
// git processes: one walk of the trunk side serves the ancestor and subject
// steps, and the squash step stops when no trunk commit touches its files.
func TestContainsUnmergedIsCheap(t *testing.T) {
	r, _, own, fork := containFixture(t, 2)
	r.Git(r.Dir, "switch", "main")
	r.Commit(r.Dir, "unrelated", map[string]string{"z.txt": "z\n"})
	run := &countingRunner{}
	m := &Matcher{R: run, Dir: r.Dir}
	target, branch := r.Git(r.Dir, "rev-parse", "main"), r.Git(r.Dir, "rev-parse", "feature/a")
	c, err := m.Contains(context.Background(), target, branch, fork, own)
	if err != nil || c.State != "none" {
		t.Fatalf("%+v, %v", c, err)
	}
	if n := run.n.Load(); n > 4 {
		t.Fatalf("%d git processes:%s", n, run.breakdown())
	}
}

// With more own commits than are listed, fork..branch is not known in full:
// the steps then read the target side separately (rev-list for ancestry,
// branch..target for subjects).
func TestContainsWithOwnCutShort(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	var s strings.Builder
	for i := 0; i < maxOwnCommits+5; i++ {
		from := ""
		if i == 0 {
			from = "from refs/heads/main^0\n"
		}
		body := fmt.Sprintf("%d\n", i)
		fmt.Fprintf(&s, "commit refs/heads/feature/a\ncommitter T <t@example.com> %d +0000\ndata 4\nc%03d\n%sM 644 inline f%d.txt\ndata %d\n%s\n",
			1767300000+i*60, i, from, i, len(body), body)
	}
	cmd := exec.Command("git", "fast-import", "--quiet")
	cmd.Dir, cmd.Env, cmd.Stdin = r.Dir, r.Env(), strings.NewReader(s.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fast-import: %v %s", err, out)
	}
	newest := fmt.Sprintf("c%03d", maxOwnCommits+4)
	r.Commit(r.Dir, newest, map[string]string{"other.txt": "o\n"})
	run := &countingRunner{}
	m := &Matcher{R: run, Dir: r.Dir}
	own, fork, _, err := m.OwnCommits(context.Background(), "main", "feature/a")
	if err != nil || len(own) != maxOwnCommits {
		t.Fatalf("own %d, %v", len(own), err)
	}
	oldOwn, oldFork, _, err := legacyOwnCommits(context.Background(), m, "main", "feature/a")
	if err != nil || oldFork != fork || !reflect.DeepEqual(oldOwn, own) {
		t.Fatalf("the own list differs from the old one: %v", err)
	}
	run.reset()
	c, err := m.containsRefs(context.Background(), "main", "feature/a", fork, own)
	if err != nil {
		t.Fatal(err)
	}
	if c.State != "some" || c.Matched != 1 || c.Per[0].By != "subject" || c.Per[0].Subject != newest {
		t.Fatalf("%+v", c.Per[0])
	}
	if !strings.Contains(run.breakdown(), "rev-list=") {
		t.Fatalf("the separate ancestor walk did not run:%s", run.breakdown())
	}
}

// The fork's commit time comes with the own commits (as the --boundary line of
// the same log): two processes, not three.
func TestOwnCommitsReadsForkTimeInTheSameLog(t *testing.T) {
	r, _, own, fork := containFixture(t, 2)
	run := &countingRunner{}
	m := &Matcher{R: run, Dir: r.Dir}
	got, gotFork, forkTime, err := m.OwnCommits(context.Background(), "main", "feature/a")
	if err != nil || gotFork != fork || len(got) != len(own) || got[0] != own[0] {
		t.Fatalf("own %+v fork %s, %v", got, gotFork, err)
	}
	if want := r.Git(r.Dir, "log", "-1", "--format=%ct", fork); strconv.FormatInt(forkTime, 10) != want {
		t.Fatalf("fork time %d, want %s", forkTime, want)
	}
	if n := run.n.Load(); n != 2 {
		t.Fatalf("%d git processes:%s", n, run.breakdown())
	}
}

// mergeBaseRunner answers merge-base with a fixed failure.
type mergeBaseRunner struct {
	gitx.ExecRunner
	err error
}

func (m mergeBaseRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if args[0] == "merge-base" {
		return nil, m.err
	}
	return m.ExecRunner.Run(ctx, dir, args...)
}

// Only a merge-base that exits 1 with no output and nothing on stderr means
// "no common history"; anything else is an error, never cached as such.
func TestOwnCommitsNoForkOnlyOnCleanExit1(t *testing.T) {
	r, _, _, _ := containFixture(t, 1)
	quiet := &gitx.GitError{Args: []string{"merge-base"}, ExitCode: 1}
	loud := &gitx.GitError{Args: []string{"merge-base"}, ExitCode: 1, Stderr: "fatal: something went wrong\n"}
	if _, _, _, err := (&Matcher{R: mergeBaseRunner{err: quiet}, Dir: r.Dir}).OwnCommits(context.Background(), "main", "feature/a"); !errors.Is(err, errNoForkPoint) {
		t.Fatalf("clean exit 1: %v, want no fork point", err)
	}
	if _, _, _, err := (&Matcher{R: mergeBaseRunner{err: loud}, Dir: r.Dir}).OwnCommits(context.Background(), "main", "feature/a"); err == nil || errors.Is(err, errNoForkPoint) {
		t.Fatalf("exit 1 with stderr: %v, want an error", err)
	}
}
