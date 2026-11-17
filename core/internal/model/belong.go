package model

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/banma05/git-glance/core/internal/gitx"
)

// AheadBehind compares a branch with a trunk: Ahead is the number of commits
// in the branch that the trunk lacks, Behind the other way round.
type AheadBehind struct {
	Ahead  int `json:"ahead"`
	Behind int `json:"behind"`
}

// Counter counts ahead/behind for many local branches against one trunk ref.
// Keys of the result are the branch names exactly as passed in.
type Counter interface {
	Counts(ctx context.Context, branches []string, trunkRef string) (map[string]AheadBehind, error)
}

// NewCounter picks the single-call path on git 2.41+ and the per-branch
// rev-list path otherwise; both give identical results.
func NewCounter(r gitx.Runner, repoRoot string, v gitx.Version) Counter {
	if v.AtLeast(2, 41) {
		return bulkCounter{r, repoRoot}
	}
	return perBranchCounter{r, repoRoot}
}

type bulkCounter struct {
	r   gitx.Runner
	dir string
}

func (c bulkCounter) Counts(ctx context.Context, branches []string, trunkRef string) (map[string]AheadBehind, error) {
	out, err := c.r.Run(ctx, c.dir, "for-each-ref",
		"--format=%(refname:short)%00%(ahead-behind:"+trunkRef+")", "refs/heads")
	if err != nil {
		return nil, err
	}
	all := map[string]AheadBehind{}
	for _, line := range strings.Split(string(out), "\n") {
		name, counts, ok := strings.Cut(strings.TrimSpace(line), "\x00")
		if !ok {
			continue
		}
		ab, err := parseCounts(counts, false)
		if err != nil {
			return nil, err
		}
		all[name] = ab
	}
	res := make(map[string]AheadBehind, len(branches))
	for _, b := range branches {
		if ab, ok := all[b]; ok {
			res[b] = ab
		}
	}
	return res, nil
}

type perBranchCounter struct {
	r   gitx.Runner
	dir string
}

func (c perBranchCounter) Counts(ctx context.Context, branches []string, trunkRef string) (map[string]AheadBehind, error) {
	res := make(map[string]AheadBehind, len(branches))
	for _, b := range branches {
		out, err := c.r.Run(ctx, c.dir, "rev-list", "--left-right", "--count",
			trunkRef+"...refs/heads/"+b)
		if err != nil {
			return nil, err
		}
		ab, err := parseCounts(string(out), true)
		if err != nil {
			return nil, err
		}
		res[b] = ab
	}
	return res, nil
}

// parseCounts reads "<a> <b>". For rev-list the pair is (behind, ahead); for
// ahead-behind it is (ahead, behind).
func parseCounts(s string, leftIsBehind bool) (AheadBehind, error) {
	f := strings.Fields(s)
	if len(f) != 2 {
		return AheadBehind{}, fmt.Errorf("unexpected counts %q", s)
	}
	a, err1 := strconv.Atoi(f[0])
	b, err2 := strconv.Atoi(f[1])
	if err1 != nil || err2 != nil {
		return AheadBehind{}, fmt.Errorf("unexpected counts %q", s)
	}
	if leftIsBehind {
		return AheadBehind{Ahead: b, Behind: a}, nil
	}
	return AheadBehind{Ahead: a, Behind: b}, nil
}

// Assignment says which trunk a branch belongs to and how that was decided:
// count, created, manual or unknown (Trunk empty).
type Assignment struct {
	Trunk  string `json:"trunk"`
	Source string `json:"source"`
}

// AssignTrunk picks the trunk with the smallest Ahead; ties are broken by the
// trunk the branch was created from, then by a manual choice.
func AssignTrunk(ahead map[string]int, order []string, created, manual string) Assignment {
	best := -1
	var tied []string
	for _, name := range order {
		n, ok := ahead[name]
		if !ok {
			continue
		}
		switch {
		case best < 0 || n < best:
			best, tied = n, []string{name}
		case n == best:
			tied = append(tied, name)
		}
	}
	if len(tied) == 1 {
		return Assignment{Trunk: tied[0], Source: "count"}
	}
	if has(tied, created) {
		return Assignment{Trunk: created, Source: "created"}
	}
	if has(tied, manual) {
		return Assignment{Trunk: manual, Source: "manual"}
	}
	return Assignment{Source: "unknown"}
}

func has(list []string, s string) bool {
	if s == "" {
		return false
	}
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// IsOld reports whether a branch counts as stale.
func IsOld(inFolder, merged bool, lastCommit, now int64, oldDays int) bool {
	if oldDays <= 0 {
		oldDays = 30
	}
	return !inFolder && (merged || now-lastCommit > int64(oldDays)*86400)
}
