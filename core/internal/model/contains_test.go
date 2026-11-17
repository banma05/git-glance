package model

import (
	"context"
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
	_, m, _, fork := containFixture(t, 1)
	c, err := m.Contains(context.Background(), "main", "feature/a", fork, nil)
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
	c, err := m.Contains(context.Background(), "main", "feature/a", fork, own)
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
	c, err := m.Contains(context.Background(), "main-test", "feature/a", fork, own)
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
	c, err := m.Contains(context.Background(), "main-test", "feature/a", fork, own)
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
	c, err := m.Contains(context.Background(), "main-test", "feature/a", fork, own)
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
	c, err := m.Contains(context.Background(), "main", "feature/a", fork, own)
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
	c, err := m.Contains(context.Background(), "main", "feature/a", fork, own)
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
	c, err := m.Contains(context.Background(), "main-test", "feature/a", fork, own)
	if err != nil {
		t.Fatal(err)
	}
	if c.State != "some" || c.Matched != 1 || c.Total != 2 {
		t.Fatalf("%+v", c)
	}
	if c.Per[0].By != "" || c.Per[1].By != "patch" {
		t.Fatalf("%v", bys(c))
	}
	again, err := m.Contains(context.Background(), "main-test", "feature/a", fork, own)
	if err != nil || again.State != "some" {
		t.Fatalf("cached: %+v %v", again, err)
	}
}

func TestContainsNone(t *testing.T) {
	_, m, own, fork := containFixture(t, 2)
	c, err := m.Contains(context.Background(), "main-test", "feature/a", fork, own)
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
