package model

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/repo"
	"github.com/banma05/git-glance/core/internal/settings"
	"github.com/banma05/git-glance/core/internal/testrepo"
)

func buildView(t *testing.T, dir string, rs settings.RepoSettings, now int64) View {
	t.Helper()
	ctx := context.Background()
	runner := gitx.ExecRunner{}
	v, err := gitx.DetectVersion(ctx, runner)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := repo.Read(ctx, runner, dir)
	if err != nil {
		t.Fatal(err)
	}
	view, err := Build(ctx, runner, v, snap, rs, now)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func listHas(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestBuildTwoTrunks(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	checkout := func(dir, branch string) { r.Git(dir, "checkout", "-q", branch) }

	// main and v2 each get two commits after v2 is cut from main.
	r.Branch("v2", "main")
	r.Commit(r.Dir, "main one", map[string]string{"m1.txt": "m1\n"})
	r.Commit(r.Dir, "main two", map[string]string{"m2.txt": "m2\n"})
	checkout(r.Dir, "v2")
	r.Commit(r.Dir, "v2 one", map[string]string{"v1.txt": "v1\n"})
	r.Commit(r.Dir, "v2 two", map[string]string{"v2.txt": "v2\n"})
	checkout(r.Dir, "main")
	r.Push(r.Dir, "origin", "main", "v2")
	r.Branch("main-test", "main")
	r.Push(r.Dir, "origin", "main-test")
	r.Branch("alice/fresh", r.Git(r.Dir, "merge-base", "main", "v2"))

	// alice/v1/login: in a folder with a new file, cherry-picked into main-test.
	r.Git(r.Dir, "branch", "--no-track", "alice/v1/login", "origin/main")
	loginDir := r.AddWorktree("login", "alice/v1/login")
	loginHash := r.Commit(loginDir, "login on v1", map[string]string{"login.txt": "login\n"})
	if err := os.WriteFile(filepath.Join(loginDir, "notes.txt"), []byte("draft\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	checkout(r.Dir, "main-test")
	r.Git(r.Dir, "cherry-pick", loginHash)
	r.Push(r.Dir, "origin", "main-test")
	checkout(r.Dir, "main")

	// alice/v2/login: no folder.
	r.Git(r.Dir, "branch", "--no-track", "alice/v2/login", "origin/v2")
	checkout(r.Dir, "alice/v2/login")
	r.Commit(r.Dir, "login on v2", map[string]string{"login-v2.txt": "login\n"})
	checkout(r.Dir, "main")

	// alice/v1/done: pushed, merged into main, in a clean folder.
	r.Branch("alice/v1/done", "main")
	checkout(r.Dir, "alice/v1/done")
	r.Commit(r.Dir, "finish done", map[string]string{"done.txt": "done\n"})
	r.Push(r.Dir, "origin", "alice/v1/done")
	checkout(r.Dir, "main")
	r.Git(r.Dir, "merge", "--ff-only", "alice/v1/done")
	r.Push(r.Dir, "origin", "main")
	r.AddWorktree("done", "alice/v1/done")

	// alice/v1/stale: one commit 40 days before now, no folder.
	now := r.Clock().Add(time.Hour).Unix()
	r.Branch("alice/v1/stale", "main")
	checkout(r.Dir, "alice/v1/stale")
	r.CommitAt(r.Dir, "old work", map[string]string{"stale.txt": "stale\n"}, time.Unix(now, 0).Add(-40*24*time.Hour))
	checkout(r.Dir, "main")

	rs := settings.RepoSettings{
		Trunks:     []settings.TrunkConfig{{Name: "main", Test: "main-test"}, {Name: "v2"}},
		HidePrefix: "alice/",
	}
	v := buildView(t, r.Dir, rs, now)

	login := v.Branches["alice/v1/login"]
	if login.Trunk != "main" || login.TrunkSource != "count" {
		t.Fatalf("login trunk: %q %q", login.Trunk, login.TrunkSource)
	}
	if login.Short != "v1/login" || login.Topic != "login" {
		t.Fatalf("login names: %q %q", login.Short, login.Topic)
	}
	if login.CollapseBlock != "dirty" || login.CanCollapse {
		t.Fatalf("login collapse: %v %q", login.CanCollapse, login.CollapseBlock)
	}
	if login.Test == nil || login.Test.State != "all" {
		t.Fatalf("login test: %+v", login.Test)
	}
	if login.HasUpstream || login.Unpushed != 1 {
		t.Fatalf("login upstream %v unpushed %d", login.HasUpstream, login.Unpushed)
	}

	v2login := v.Branches["alice/v2/login"]
	if v2login.Trunk != "v2" {
		t.Fatalf("v2 login trunk: %q", v2login.Trunk)
	}
	if v2login.Color != login.Color {
		t.Fatalf("colours differ: %d vs %d", v2login.Color, login.Color)
	}

	done := v.Branches["alice/v1/done"]
	if !done.Merged || !done.CanCollapse {
		t.Fatalf("done: merged %v canCollapse %v (%q)", done.Merged, done.CanCollapse, done.CollapseBlock)
	}
	if done.Release == nil || done.Release.State != "all" || !reflect.DeepEqual(done.Release.In, []string{"main"}) {
		t.Fatalf("done release: %+v", done.Release)
	}

	stale, ok := v.Branches["alice/v1/stale"]
	if !ok || !stale.Old {
		t.Fatalf("stale: present %v old %v", ok, stale.Old)
	}
	for _, g := range v.Groups {
		if listHas(g.Branches, "alice/v1/stale") {
			t.Fatalf("stale in group %s", g.Trunk.Name)
		}
	}
	if listHas(v.Unknown, "alice/v1/stale") || !listHas(v.Old, "alice/v1/stale") {
		t.Fatalf("stale: unknown %v old %v", v.Unknown, v.Old)
	}

	if !listHas(v.Unknown, "alice/fresh") {
		t.Fatalf("fresh not unknown: %v", v.Unknown)
	}

	if len(v.Groups) != 2 || v.Groups[0].Trunk.Name != "main" || v.Groups[1].Trunk.Name != "v2" {
		t.Fatalf("groups: %+v", v.Groups)
	}
	if len(v.Sections) != 2 {
		t.Fatalf("sections: %d", len(v.Sections))
	}

	rs.CreatedBase = map[string]string{"alice/fresh": "v2"}
	v = buildView(t, r.Dir, rs, now)
	fresh := v.Branches["alice/fresh"]
	if fresh.Trunk != "v2" || fresh.TrunkSource != "created" {
		t.Fatalf("fresh: %q %q", fresh.Trunk, fresh.TrunkSource)
	}
}

func TestBuildNoRemote(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	dir := r.Root + "/solo"
	r.Git(r.Root, "init", "-q", dir)
	r.Commit(dir, "first", map[string]string{"a.txt": "a\n"})
	r.Git(dir, "checkout", "-q", "-b", "feature/x")
	r.Commit(dir, "x", map[string]string{"x.txt": "x\n"})
	r.Git(dir, "checkout", "-q", "main")
	now := r.Clock().Add(time.Hour).Unix()

	v := buildView(t, dir, settings.RepoSettings{}, now)
	if len(v.Groups) == 0 || v.Groups[0].Trunk.Ref != "main" {
		t.Fatalf("groups: %+v", v.Groups)
	}
	if !listHas(v.Groups[0].Branches, "feature/x") {
		t.Fatalf("feature/x not in main: %v", v.Groups[0].Branches)
	}
	if v.Branches["feature/x"].HasUpstream {
		t.Fatal("feature/x has upstream")
	}

	// Empty lists reach the UI as [] (never null), with camelCase keys.
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"unknown":[]`, `"old":[]`, `"trunks":[]`, `"releases":[]`, `"behindTrunk":`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("JSON lacks %s: %s", want, data)
		}
	}
}

func TestBuildUnrelatedHistory(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	r.Git(r.Dir, "checkout", "-q", "--orphan", "pages")
	r.Commit(r.Dir, "site", map[string]string{"index.html": "site\n"})
	r.Git(r.Dir, "checkout", "-q", "main")
	now := r.Clock().Add(time.Hour).Unix()

	v := buildView(t, r.Dir, settings.RepoSettings{}, now)
	pages := v.Branches["pages"]
	if pages.Trunk != "" || pages.TrunkSource != "unknown" || pages.ForkHash != "" || len(pages.Own) != 0 {
		t.Fatalf("pages: %+v", pages)
	}
	if !listHas(v.Unknown, "pages") || len(v.Sections) != 1 {
		t.Fatalf("unknown %v sections %d", v.Unknown, len(v.Sections))
	}
}

func TestBuildSameNamedTags(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	base := r.Git(r.Dir, "rev-parse", "HEAD")
	r.Branch("feature/x", "main")
	r.Git(r.Dir, "checkout", "-q", "feature/x")
	tip := r.Commit(r.Dir, "x", map[string]string{"x.txt": "x\n"})
	r.Git(r.Dir, "checkout", "-q", "main")
	r.Commit(r.Dir, "main work", map[string]string{"m.txt": "m\n"})
	r.Push(r.Dir, "origin", "main")
	// Short names would resolve to these tags instead of the branches.
	r.Git(r.Dir, "tag", "feature/x", base)
	r.Git(r.Dir, "tag", "origin/main", base)
	now := r.Clock().Add(time.Hour).Unix()

	ctx := context.Background()
	runner := gitx.ExecRunner{}
	snap, err := repo.Read(ctx, runner, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	// The trunk is configured: the tag also hides origin/HEAD's short name.
	rs := settings.RepoSettings{Trunks: []settings.TrunkConfig{{Name: "main"}}}
	for _, ver := range counterVersions(t) {
		v, err := Build(ctx, runner, ver, snap, rs, now)
		if err != nil {
			t.Fatalf("%v: %v", ver, err)
		}
		x := v.Branches["feature/x"]
		if x.Trunk != "main" || len(x.Own) != 1 || x.Own[0].Hash != tip || x.ForkHash != base {
			t.Fatalf("%v: %+v", ver, x)
		}
		if x.BehindTrunk != 1 || x.Merged || v.Groups[0].Trunk.LocalAhead != 0 || v.Groups[0].Trunk.LocalBehind != 0 {
			t.Fatalf("%v: behind %d merged %v trunk %+v", ver, x.BehindTrunk, x.Merged, v.Groups[0].Trunk)
		}
	}
}
