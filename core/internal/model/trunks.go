// Package model derives the branch-panel view from a repository snapshot:
// trunks, which trunk a branch belongs to, names, colours and staleness.
package model

import (
	"strings"

	"github.com/banma05/git-glance/core/internal/repo"
	"github.com/banma05/git-glance/core/internal/settings"
)

// Target is a resolved branch: its configured name, the ref that was found
// for it, and that ref's tip.
type Target struct {
	Name string `json:"name"`
	Ref  string `json:"ref"`
	Hash string `json:"hash"`
	Time int64  `json:"time"`
}

// Trunk is a long-lived branch with its optional test and release branches.
type Trunk struct {
	Target
	Test     *Target  `json:"test"`
	Releases []Target `json:"releases"`
}

// ResolveTrunks turns the configured trunks into resolved ones. Without
// configuration it falls back to the remote HEAD, then local main, then
// master. Trunks that exist nowhere are skipped.
func ResolveTrunks(s repo.Snapshot, rs settings.RepoSettings) []Trunk {
	cfg := rs.Trunks
	if len(cfg) == 0 {
		cfg = []settings.TrunkConfig{{Name: defaultTrunkName(s)}}
	}
	out := []Trunk{}
	for _, c := range cfg {
		t, ok := resolveTarget(s, c.Name)
		if !ok {
			continue
		}
		tr := Trunk{Target: t, Releases: []Target{}}
		if c.Test != "" {
			if x, ok := resolveTarget(s, c.Test); ok {
				tr.Test = &x
			}
		}
		for _, n := range c.Releases {
			if x, ok := resolveTarget(s, n); ok {
				tr.Releases = append(tr.Releases, x)
			}
		}
		out = append(out, tr)
	}
	return out
}

func defaultTrunkName(s repo.Snapshot) string {
	if s.RemoteHead != "" {
		return s.RemoteHead
	}
	for _, r := range s.Local {
		if r.Name == "main" {
			return "main"
		}
	}
	return "master"
}

func resolveTarget(s repo.Snapshot, name string) (Target, bool) {
	if name == "" {
		return Target{}, false
	}
	if r, ok := findRef(s.Remote, "origin/"+name); ok {
		return toTarget(name, r), true
	}
	for _, r := range s.Remote {
		if i := strings.Index(r.Name, "/"); i > 0 && r.Name[i+1:] == name {
			return toTarget(name, r), true
		}
	}
	if r, ok := findRef(s.Local, name); ok {
		return toTarget(name, r), true
	}
	return Target{}, false
}

func findRef(refs []repo.Ref, name string) (repo.Ref, bool) {
	for _, r := range refs {
		if r.Name == name {
			return r, true
		}
	}
	return repo.Ref{}, false
}

func toTarget(name string, r repo.Ref) Target {
	return Target{Name: name, Ref: r.Name, Hash: r.Hash, Time: r.Time}
}
