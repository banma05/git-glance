package model

import (
	"context"

	"github.com/banma05/git-glance/core/internal/gitx"
)

// NewCounter is the reference counter: the single-call path on git 2.41+ and,
// before that, one `rev-list --left-right --count` per branch by ref name, as
// builds did before they asked by hashes. Tests compare against it.
func NewCounter(r gitx.Runner, repoRoot string, v gitx.Version) Counter {
	if v.AtLeast(2, 41) {
		return bulkCounter{r, repoRoot}
	}
	return perBranchCounter{r, repoRoot}
}

type perBranchCounter struct {
	r   gitx.Runner
	dir string
}

func (c perBranchCounter) Counts(ctx context.Context, branches []string, trunkRef string) (map[string]AheadBehind, error) {
	res := make(map[string]AheadBehind, len(branches))
	for _, b := range branches {
		// "--" makes a missing branch a bad-revision error, never a path.
		out, err := c.r.Run(ctx, c.dir, "rev-list", "--left-right", "--count",
			trunkRef+"...refs/heads/"+b, "--")
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
