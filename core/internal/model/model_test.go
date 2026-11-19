package model

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/repo"
	"github.com/banma05/git-glance/core/internal/settings"
	"github.com/banma05/git-glance/core/internal/testrepo"
)

func TestAssignTrunk(t *testing.T) {
	order := []string{"main", "v2", "v3"}
	cases := []struct {
		name            string
		ahead           map[string]int
		created, manual string
		recorded        string
		want            Assignment
	}{
		{"unique min", map[string]int{"main": 1, "v2": 248}, "", "", "", Assignment{"main", "count"}},
		{"tie created", map[string]int{"main": 0, "v2": 0}, "v2", "", "", Assignment{"v2", "created"}},
		{"tie manual", map[string]int{"main": 0, "v2": 0}, "", "main", "", Assignment{"main", "manual"}},
		{"tie none", map[string]int{"main": 0, "v2": 0}, "", "", "", Assignment{"", "unknown"}},
		{"tie recorded", map[string]int{"main": 0, "v2": 0}, "", "", "v2", Assignment{"v2", "reflog"}},
		{"created beats recorded", map[string]int{"main": 0, "v2": 0}, "main", "", "v2", Assignment{"main", "created"}},
		{"manual beats recorded", map[string]int{"main": 0, "v2": 0}, "", "main", "v2", Assignment{"main", "manual"}},
		{"recorded outside the tie", map[string]int{"main": 2, "v2": 2, "v3": 5}, "", "", "v3", Assignment{"", "unknown"}},
		{"unique count ignores recorded", map[string]int{"main": 1, "v2": 3}, "", "", "v2", Assignment{"main", "count"}},
		{"tie created outside", map[string]int{"main": 2, "v2": 2}, "v3", "", "", Assignment{"", "unknown"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := AssignTrunk(c.ahead, order, c.created, c.manual, c.recorded); got != c.want {
				t.Fatalf("got %+v want %+v", got, c.want)
			}
		})
	}
}

func revCount(t *testing.T, r *testrepo.Repo, spec string) int {
	t.Helper()
	n, err := strconv.Atoi(strings.TrimSpace(r.Git(r.Dir, "rev-list", "--count", spec)))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCounterPathsAgree(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	r.Branch("v2", "main")
	r.Branch("feature/a", "main")
	r.Branch("feature/b", "v2")
	r.Branch("feature/c", "main")
	commit := func(branch, file string) {
		r.Git(r.Dir, "checkout", branch)
		r.Commit(r.Dir, "c "+file, map[string]string{file: file + "\n"})
	}
	commit("v2", "v2-1")
	commit("v2", "v2-2")
	r.Push(r.Dir, "origin", "v2")
	commit("feature/a", "a1")
	commit("feature/a", "a2")
	commit("feature/b", "b1")
	commit("main", "m1")
	branches := []string{"feature/a", "feature/b", "feature/c"}
	ctx := context.Background()
	var results []map[string]AheadBehind
	for _, v := range []gitx.Version{{Major: 2, Minor: 40}, {Major: 2, Minor: 41}} {
		if v.AtLeast(2, 41) {
			cur, err := gitx.DetectVersion(ctx, gitx.ExecRunner{})
			if err != nil || !cur.AtLeast(2, 41) {
				continue
			}
		}
		got, err := NewCounter(gitx.ExecRunner{}, r.Dir, v).Counts(ctx, branches, "main")
		if err != nil {
			t.Fatal(err)
		}
		results = append(results, got)
	}
	for _, got := range results[1:] {
		if !reflect.DeepEqual(got, results[0]) {
			t.Fatalf("paths differ: %v vs %v", got, results[0])
		}
	}
	if want := revCount(t, r, "main..feature/a"); results[0]["feature/a"].Ahead != want || want != 2 {
		t.Fatalf("feature/a ahead = %d, want %d", results[0]["feature/a"].Ahead, want)
	}
	if results[0]["feature/a"].Behind != 1 {
		t.Fatalf("feature/a behind = %d", results[0]["feature/a"].Behind)
	}
	if len(results[0]) != 3 {
		t.Fatalf("keys: %v", results[0])
	}
}

func TestShortNameTopic(t *testing.T) {
	if ShortName("alice/feature/login", "alice/") != "feature/login" {
		t.Fatal("prefix not removed")
	}
	if ShortName("bob/x", "alice/") != "bob/x" {
		t.Fatal("foreign prefix removed")
	}
	if ShortName("alice/x", "") != "alice/x" {
		t.Fatal("empty prefix")
	}
	if Topic("alice/v2/login") != "login" || Topic("main") != "main" {
		t.Fatal("topic")
	}
}

func TestAssignColorsStable(t *testing.T) {
	in := []string{"a", "b", "c", "a"}
	x, y := AssignColors(in), AssignColors(in)
	if !reflect.DeepEqual(x, y) || len(x) != 3 {
		t.Fatalf("unstable: %v %v", x, y)
	}
	ab := AssignColors([]string{"a", "b"})
	abc := AssignColors([]string{"a", "b", "c"})
	if abc["a"] != ab["a"] || abc["b"] != ab["b"] {
		t.Fatalf("existing colours moved: %v -> %v", ab, abc)
	}
	var many []string
	for i := 0; i < 13; i++ {
		many = append(many, "topic"+strconv.Itoa(i))
	}
	for k, v := range AssignColors(many) {
		if v < 0 || v >= PaletteSize {
			t.Fatalf("%s: %d out of range", k, v)
		}
	}
}

func TestResolveTrunksDefaults(t *testing.T) {
	s := repo.Snapshot{
		RemoteHead: "main",
		Local:      []repo.Ref{{Name: "main", Hash: "h1"}},
		Remote:     []repo.Ref{{Name: "origin/main", Hash: "h2", Time: 5}},
	}
	got := ResolveTrunks(s, settings.RepoSettings{})
	if len(got) != 1 || got[0].Ref != "origin/main" || got[0].Hash != "h2" || got[0].Name != "main" {
		t.Fatalf("%+v", got)
	}
	s = repo.Snapshot{Local: []repo.Ref{{Name: "master"}}}
	got = ResolveTrunks(s, settings.RepoSettings{})
	if len(got) != 1 || got[0].Ref != "master" {
		t.Fatalf("%+v", got)
	}
}

func TestResolveTrunksConfigured(t *testing.T) {
	s := repo.Snapshot{
		Local: []repo.Ref{{Name: "v2"}},
		Remote: []repo.Ref{
			{Name: "origin/main"}, {Name: "up/stage"}, {Name: "origin/rel-1"},
		},
	}
	rs := settings.RepoSettings{Trunks: []settings.TrunkConfig{
		{Name: "main", Test: "stage", Releases: []string{"rel-1", "gone"}},
		{Name: "v2"},
		{Name: "missing"},
	}}
	got := ResolveTrunks(s, rs)
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	if got[0].Test == nil || got[0].Test.Ref != "up/stage" {
		t.Fatalf("test: %+v", got[0].Test)
	}
	if len(got[0].Releases) != 1 || got[0].Releases[0].Ref != "origin/rel-1" {
		t.Fatalf("releases: %+v", got[0].Releases)
	}
	if got[1].Ref != "v2" || got[1].Releases == nil || got[1].Test != nil {
		t.Fatalf("%+v", got[1])
	}
}

func TestIsOld(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).Unix()
	day := int64(86400)
	if IsOld(true, true, now-90*day, now, 30) {
		t.Fatal("in folder")
	}
	if !IsOld(false, true, now, now, 30) {
		t.Fatal("merged")
	}
	if !IsOld(false, false, now-31*day, now, 30) {
		t.Fatal("31 days")
	}
	if IsOld(false, false, now-29*day, now, 30) {
		t.Fatal("29 days")
	}
	if IsOld(false, false, now-29*day, now, 0) || !IsOld(false, false, now-31*day, now, 0) {
		t.Fatal("default days")
	}
}

// counterVersions lists the counter paths this git can run: always the
// per-branch path, plus the single-call path on git 2.41+.
func counterVersions(t *testing.T) []gitx.Version {
	t.Helper()
	versions := []gitx.Version{{Major: 2, Minor: 40}}
	cur, err := gitx.DetectVersion(context.Background(), gitx.ExecRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if cur.AtLeast(2, 41) {
		versions = append(versions, gitx.Version{Major: 2, Minor: 41})
	}
	return versions
}

func TestCounterSameNamedTag(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	r.Branch("v2", "main")
	r.Git(r.Dir, "checkout", "-q", "v2")
	r.Commit(r.Dir, "v2 work", map[string]string{"v2.txt": "v2\n"})
	r.Git(r.Dir, "checkout", "-q", "main")
	// The tag points at the fork point, so counting the tag instead of the
	// branch would give ahead 0.
	r.Git(r.Dir, "tag", "v2", "main")
	r.Commit(r.Dir, "main work", map[string]string{"m.txt": "m\n"})
	want := AheadBehind{Ahead: 1, Behind: 1}
	for _, v := range counterVersions(t) {
		got, err := NewCounter(gitx.ExecRunner{}, r.Dir, v).Counts(context.Background(), []string{"v2"}, "refs/heads/main")
		if err != nil {
			t.Fatalf("%v: %v", v, err)
		}
		if got["v2"] != want || len(got) != 1 {
			t.Fatalf("%v: got %v, want v2=%v", v, got, want)
		}
	}
}

func TestCounterMissingName(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	r.Git(r.Dir, "tag", "only-tag", "main")
	for _, v := range counterVersions(t) {
		for _, name := range []string{"nope", "only-tag"} {
			_, err := NewCounter(gitx.ExecRunner{}, r.Dir, v).Counts(context.Background(), []string{"main", name}, "refs/heads/main")
			if err == nil {
				t.Fatalf("%v: no error for %q", v, name)
			}
		}
	}
}
