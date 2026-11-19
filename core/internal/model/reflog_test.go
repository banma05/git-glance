package model

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/banma05/git-glance/core/internal/settings"
	"github.com/banma05/git-glance/core/internal/testrepo"
)

func twoTrunks() settings.RepoSettings {
	return settings.RepoSettings{Trunks: []settings.TrunkConfig{{Name: "main"}, {Name: "v2"}}}
}

func TestTrunkNamed(t *testing.T) {
	tied := []string{"main", "v2"}
	remotes := []string{"origin", "upstream"}
	cases := map[string]string{
		"main": "main", "refs/heads/v2": "v2", "origin/v2": "v2",
		"refs/remotes/origin/v2": "v2", "upstream/v2": "v2",
		"other/v2": "", "feature/login": "", "HEAD": "", "": "", "v3": "",
	}
	for start, want := range cases {
		if got := trunkNamed(start, tied, remotes); got != want {
			t.Errorf("%q: got %q want %q", start, got, want)
		}
	}
	if got := trunkNamed("upstream/v2", tied, []string{"origin"}); got != "" {
		t.Errorf("unconfigured remote: got %q", got)
	}
}

// v2 merges main, so a branch cut from main with one commit is one commit
// ahead of both trunks.
func TestAssignByCreationRecordMergedTrunk(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	r.Branch("v2", "main")
	r.Git(r.Dir, "checkout", "-q", "v2")
	r.Commit(r.Dir, "v2 only", map[string]string{"v2.txt": "v2\n"})
	r.Git(r.Dir, "checkout", "-q", "main")
	r.Commit(r.Dir, "main news", map[string]string{"m.txt": "m\n"})
	r.Git(r.Dir, "checkout", "-q", "v2")
	r.Git(r.Dir, "merge", "-q", "--no-edit", "main")
	r.Git(r.Dir, "checkout", "-q", "main")
	r.Push(r.Dir, "origin", "main", "v2")
	hash := r.Git(r.Dir, "rev-parse", "main")[:40]

	cut := func(name, start string) {
		r.Git(r.Dir, "checkout", "-q", "-b", name, start)
		r.Commit(r.Dir, "work on "+name, map[string]string{name + ".txt": "x\n"})
		r.Git(r.Dir, "checkout", "-q", "main")
	}
	cut("feature/from-origin-main", "origin/main")
	cut("feature/from-head", "HEAD")
	cut("feature/from-hash", hash)
	cut("feature/base", "main")
	cut("feature/on-feature", "feature/base")
	// Only a commit entry is left.
	r.Git(r.Dir, "checkout", "-q", "-b", "feature/expired", "origin/main")
	r.Git(r.Dir, "reflog", "expire", "--expire=all", "refs/heads/feature/expired")
	r.Commit(r.Dir, "work on expired", map[string]string{"expired.txt": "x\n"})
	r.Git(r.Dir, "checkout", "-q", "main")
	// No reflog written at all, and a reflog removed afterwards.
	r.Git(r.Dir, "-c", "core.logAllRefUpdates=false", "checkout", "-q", "-b", "feature/unlogged", "origin/main")
	r.Git(r.Dir, "-c", "core.logAllRefUpdates=false", "commit", "-q", "--allow-empty", "-m", "work")
	r.Git(r.Dir, "checkout", "-q", "main")
	cut("feature/removed", "origin/main")
	if err := os.Remove(filepath.Join(r.Dir, ".git", "logs", "refs", "heads", "feature", "removed")); err != nil {
		t.Fatal(err)
	}
	// A manual choice beats the record.
	cut("feature/manual", "origin/main")

	rs := twoTrunks()
	rs.ManualTrunk = map[string]string{"feature/manual": "v2"}
	v := buildView(t, r.Dir, rs, r.Clock().Unix()+3600)

	want := map[string][2]string{
		"feature/from-origin-main": {"main", "reflog"},
		"feature/from-head":        {"", "unknown"},
		"feature/from-hash":        {"", "unknown"},
		"feature/on-feature":       {"", "unknown"},
		"feature/expired":          {"", "unknown"},
		"feature/unlogged":         {"", "unknown"},
		"feature/removed":          {"", "unknown"},
		"feature/manual":           {"v2", "manual"},
	}
	for name, w := range want {
		bv, ok := v.Branches[name]
		if !ok {
			t.Fatalf("%s missing from view", name)
		}
		if bv.Trunk != w[0] || bv.TrunkSource != w[1] || bv.Error != "" {
			t.Errorf("%s: trunk %q source %q error %q, want %v", name, bv.Trunk, bv.TrunkSource, bv.Error, w)
		}
	}
}

// With both trunks at the same commit, a branch with no commits ties and the
// record names the trunk, in every spelling git writes.
func TestAssignByCreationRecordSpellings(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	r.Branch("v2", "main")
	r.Push(r.Dir, "origin", "v2")
	for name, start := range map[string]string{
		"feature/a": "origin/v2", "feature/b": "refs/remotes/origin/v2",
		"feature/c": "v2", "feature/d": "refs/heads/v2",
	} {
		r.Git(r.Dir, "branch", name, start)
	}
	v := buildView(t, r.Dir, twoTrunks(), r.Clock().Unix()+3600)
	for _, name := range []string{"feature/a", "feature/b", "feature/c", "feature/d"} {
		bv := v.Branches[name]
		if bv.Trunk != "v2" || bv.TrunkSource != "reflog" || bv.Error != "" {
			t.Errorf("%s: trunk %q source %q error %q", name, bv.Trunk, bv.TrunkSource, bv.Error)
		}
	}
}
