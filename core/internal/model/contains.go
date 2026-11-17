package model

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/repo"
)

const (
	maxOwnCommits     = 200
	maxSquashCands    = 500
	maxPathspecFiles  = 100
	byAncestor        = "ancestor"
	byPatch           = "patch"
	bySubject         = "subject"
	bySquash          = "squash"
	stateAll          = "all"
	stateSome         = "some"
	stateNone         = "none"
	logFields         = "--format=%H%x00%s%x00%ct"
	literalPathPrefix = ":(literal)"
)

// CommitMatch is one own commit and the step that found it in the target.
// By is empty when no step matched.
type CommitMatch struct {
	repo.CommitInfo
	By string `json:"by"`
}

// Containment says how many own commits of a branch are already in a target.
type Containment struct {
	State   string        `json:"state"`
	Matched int           `json:"matched"`
	Total   int           `json:"total"`
	Per     []CommitMatch `json:"per"`
}

// Matcher decides containment and caches results per target and branch tip.
// The unexported fields need no setup.
type Matcher struct {
	R   gitx.Runner
	Dir string

	mu    sync.Mutex
	cache map[string]Containment
}

func (m *Matcher) run(ctx context.Context, args ...string) (string, error) {
	out, err := m.R.Run(ctx, m.Dir, args...)
	return string(out), err
}

func (m *Matcher) resolve(ctx context.Context, ref string) (string, error) {
	out, err := m.run(ctx, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	return strings.TrimSpace(out), err
}

func parseCommits(out string) []repo.CommitInfo {
	commits := []repo.CommitInfo{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(strings.TrimRight(line, "\r"), "\x00")
		if len(f) != 3 || f[0] == "" {
			continue
		}
		ts, _ := strconv.ParseInt(f[2], 10, 64)
		commits = append(commits, repo.CommitInfo{Hash: f[0], Subject: f[1], Time: ts})
	}
	return commits
}

// OwnCommits lists the commits of branchRef that are not in trunkRef, newest
// first and at most 200, with the fork point and its commit time.
func (m *Matcher) OwnCommits(ctx context.Context, trunkRef, branchRef string) ([]repo.CommitInfo, string, int64, error) {
	out, err := m.run(ctx, "merge-base", trunkRef, branchRef)
	fork := strings.TrimSpace(out)
	if err != nil || fork == "" {
		if err == nil {
			err = fmt.Errorf("no merge base between %s and %s", trunkRef, branchRef)
		}
		return nil, "", 0, err
	}
	out, err = m.run(ctx, "log", "-1", "--format=%ct", fork, "--")
	if err != nil {
		return nil, "", 0, err
	}
	forkTime, _ := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	out, err = m.run(ctx, "log", logFields, "-n", strconv.Itoa(maxOwnCommits), fork+".."+branchRef, "--")
	if err != nil {
		return nil, "", 0, err
	}
	return parseCommits(out), fork, forkTime, nil
}

type step func(ctx context.Context, target, branch, fork string, per []CommitMatch) error

// Contains finds, step by step, which own commits are already in targetRef.
func (m *Matcher) Contains(ctx context.Context, targetRef, branchRef, forkHash string, own []repo.CommitInfo) (Containment, error) {
	key, err := m.cacheKey(ctx, targetRef, branchRef, forkHash)
	if err != nil {
		return Containment{}, err
	}
	if c, ok := m.cached(key); ok {
		return c, nil
	}
	per := make([]CommitMatch, len(own))
	for i, c := range own {
		per[i] = CommitMatch{CommitInfo: c}
	}
	for _, s := range []step{m.stepAncestor, m.stepPatch, m.stepSubject, m.stepSquash} {
		if len(unmatched(per)) == 0 {
			break
		}
		if err := s(ctx, targetRef, branchRef, forkHash, per); err != nil {
			return Containment{}, err
		}
	}
	result := summarize(per)
	m.store(key, result)
	return result, nil
}

func summarize(per []CommitMatch) Containment {
	c := Containment{Total: len(per), Per: per}
	for _, p := range per {
		if p.By != "" {
			c.Matched++
		}
	}
	switch {
	case c.Matched == c.Total:
		c.State = stateAll
	case c.Matched == 0:
		c.State = stateNone
	default:
		c.State = stateSome
	}
	return c
}

func unmatched(per []CommitMatch) []int {
	var idx []int
	for i, p := range per {
		if p.By == "" {
			idx = append(idx, i)
		}
	}
	return idx
}

func (m *Matcher) cacheKey(ctx context.Context, targetRef, branchRef, forkHash string) (string, error) {
	target, err := m.resolve(ctx, targetRef)
	if err != nil {
		return "", err
	}
	branch, err := m.resolve(ctx, branchRef)
	if err != nil {
		return "", err
	}
	return target + "\x00" + branch + "\x00" + forkHash, nil
}

func (m *Matcher) cached(key string) (Containment, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.cache[key]
	if ok {
		c.Per = append([]CommitMatch{}, c.Per...)
	}
	return c, ok
}

func (m *Matcher) store(key string, c Containment) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cache == nil {
		m.cache = map[string]Containment{}
	}
	c.Per = append([]CommitMatch{}, c.Per...)
	m.cache[key] = c
}

func (m *Matcher) stepAncestor(ctx context.Context, target, branch, _ string, per []CommitMatch) error {
	out, err := m.run(ctx, "rev-list", target+".."+branch, "--")
	if err != nil {
		return err
	}
	outside := map[string]bool{}
	for _, h := range strings.Fields(out) {
		outside[h] = true
	}
	for _, i := range unmatched(per) {
		if !outside[per[i].Hash] {
			per[i].By = byAncestor
		}
	}
	return nil
}

func (m *Matcher) stepPatch(ctx context.Context, target, branch, fork string, per []CommitMatch) error {
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

func (m *Matcher) stepSubject(ctx context.Context, target, _, fork string, per []CommitMatch) error {
	out, err := m.run(ctx, "log", "--format=%s", fork+".."+target, "--")
	if err != nil {
		return err
	}
	subjects := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		subjects[strings.TrimSpace(line)] = true
	}
	for _, i := range unmatched(per) {
		if s := strings.TrimSpace(per[i].Subject); s != "" && subjects[s] {
			per[i].By = bySubject
		}
	}
	return nil
}

// patchID fingerprints the output of a read-only git command; an empty output
// has no fingerprint and yields "".
func (m *Matcher) patchID(ctx context.Context, gitArgs ...string) (string, error) {
	out, err := m.R.Run(ctx, m.Dir, gitArgs...)
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return "", err
	}
	id, err := m.R.RunInput(ctx, m.Dir, out, "patch-id", "--stable")
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(id))
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], nil
}

func (m *Matcher) stepSquash(ctx context.Context, target, branch, fork string, per []CommitMatch) error {
	names, err := m.run(ctx, "diff", "--name-only", "-z", fork, branch, "--")
	if err != nil {
		return err
	}
	whole, err := m.patchID(ctx, "diff", fork, branch, "--")
	if err != nil || whole == "" {
		return err
	}
	args := []string{"log", "--format=%H", "-n", strconv.Itoa(maxSquashCands), fork + ".." + target, "--"}
	files := strings.Split(strings.Trim(names, "\x00"), "\x00")
	if len(files) <= maxPathspecFiles {
		for _, f := range files {
			args = append(args, literalPathPrefix+f)
		}
	}
	cands, err := m.run(ctx, args...)
	if err != nil {
		return err
	}
	for _, cand := range strings.Fields(cands) {
		id, err := m.patchID(ctx, "show", "--format=", cand, "--")
		if err != nil {
			return err
		}
		if id == whole {
			for _, i := range unmatched(per) {
				per[i].By = bySquash
			}
			return nil
		}
	}
	return nil
}

// Unpushed counts commits of branchRef that no remote-tracking ref has.
func (m *Matcher) Unpushed(ctx context.Context, branchRef string) (int, error) {
	out, err := m.run(ctx, "rev-list", "--count", branchRef, "--not", "--remotes", "--")
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(out))
}
