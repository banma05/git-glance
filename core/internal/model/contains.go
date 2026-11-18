package model

import (
	"context"
	"errors"
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
	hashAndSubject    = "--format=%H%x00%s"
	literalPathPrefix = ":(literal)"
	// noSignature goes on every log/show: log.showSignature=true in the user's
	// config would otherwise print signature lines into parsed output.
	noSignature = "--no-show-signature"
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

func parseCommits(out string) ([]repo.CommitInfo, error) {
	commits := []repo.CommitInfo{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(strings.TrimRight(line, "\r"), "\x00")
		if len(f) != 3 || f[0] == "" {
			continue
		}
		ts, err := strconv.ParseInt(f[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("commit time of %s: %w", f[0], err)
		}
		commits = append(commits, repo.CommitInfo{Hash: f[0], Subject: f[1], Time: ts})
	}
	return commits, nil
}

// errNoForkPoint means the branch shares no history with the trunk.
var errNoForkPoint = errors.New("no merge base")

// OwnCommits lists the commits of branchRef that are not in trunkRef (full
// ref names or commit hashes), newest first and at most 200, with the fork
// point and its commit time. Unrelated histories give an error wrapping
// errNoForkPoint.
func (m *Matcher) OwnCommits(ctx context.Context, trunkRef, branchRef string) ([]repo.CommitInfo, string, int64, error) {
	out, err := m.run(ctx, "merge-base", trunkRef, branchRef)
	fork := strings.TrimSpace(out)
	var ge *gitx.GitError
	if fork == "" && (err == nil || errors.As(err, &ge) && ge.ExitCode == 1) {
		// merge-base exits 1 with no output when there is no common ancestor.
		return nil, "", 0, fmt.Errorf("%w between %s and %s", errNoForkPoint, trunkRef, branchRef)
	}
	if err != nil {
		return nil, "", 0, err
	}
	out, err = m.run(ctx, "log", noSignature, "-1", "--format=%ct", fork, "--")
	if err != nil {
		return nil, "", 0, err
	}
	forkTime, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return nil, "", 0, fmt.Errorf("fork time: %w", err)
	}
	out, err = m.run(ctx, "log", noSignature, logFields, "-n", strconv.Itoa(maxOwnCommits), fork+".."+branchRef, "--")
	if err != nil {
		return nil, "", 0, err
	}
	commits, err := parseCommits(out)
	return commits, fork, forkTime, err
}

// Contains finds, step by step, which own commits are already in target.
// target and branch are commit hashes, so no ref can move under the steps;
// own and forkHash must come from OwnCommits for that branch tip, stale input
// is an error.
func (m *Matcher) Contains(ctx context.Context, target, branch, forkHash string, own []repo.CommitInfo) (Containment, error) {
	if !isHash(target) || !isHash(branch) {
		return Containment{}, fmt.Errorf("containment needs commit hashes, got %q and %q", target, branch)
	}
	if err := checkFresh(branch, forkHash, own); err != nil {
		return Containment{}, err
	}
	key := target + "\x00" + branch + "\x00" + forkHash
	if c, ok := m.cached(key); ok {
		return c, nil
	}
	run := &containsRun{m: m, ctx: ctx, target: target, branch: branch, fork: forkHash, own: own, per: make([]CommitMatch, len(own))}
	for i, c := range own {
		run.per[i] = CommitMatch{CommitInfo: c}
	}
	for _, step := range []func() error{run.stepAncestor, run.stepPatch, run.stepSubject, run.stepSquash} {
		if len(unmatched(run.per)) == 0 {
			break
		}
		if err := step(); err != nil {
			return Containment{}, err
		}
	}
	result := summarize(run.per)
	m.store(key, result)
	return result, nil
}

func checkFresh(branch, fork string, own []repo.CommitInfo) error {
	if len(own) > 0 && own[0].Hash != branch {
		return fmt.Errorf("own commits are stale: newest %s, branch tip %s", own[0].Hash, branch)
	}
	if fork == "" {
		return fmt.Errorf("own commits are stale: empty fork hash")
	}
	if len(own) == 0 && fork != branch {
		return fmt.Errorf("own commits are stale: none given but fork %s is not branch tip %s", fork, branch)
	}
	return nil
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

// containsRun is one Contains call: the inputs, the matches so far and the
// target's side of the fork, read once.
type containsRun struct {
	m                    *Matcher
	ctx                  context.Context
	target, branch, fork string
	own                  []repo.CommitInfo
	per                  []CommitMatch
	side                 *targetSide
}

// targetSide is fork..target with each commit's subject.
type targetSide struct {
	hashes   map[string]bool
	subjects []string // of branch..target only
}

// targetSide reads fork..target once, when own is complete (shorter than
// maxOwnCommits): own is then all of fork..branch, so branch..target is
// fork..target without the own commits, and one walk serves the ancestor and
// the subject step. It returns nil when own was cut short.
func (c *containsRun) targetSide() (*targetSide, error) {
	if c.side != nil || len(c.own) >= maxOwnCommits {
		return c.side, nil
	}
	commits, err := c.hashesAndSubjects(c.fork + ".." + c.target)
	if err != nil {
		return nil, err
	}
	own := map[string]bool{}
	for _, o := range c.own {
		own[o.Hash] = true
	}
	sd := &targetSide{hashes: map[string]bool{}}
	for _, hs := range commits {
		sd.hashes[hs[0]] = true
		if !own[hs[0]] {
			sd.subjects = append(sd.subjects, hs[1])
		}
	}
	c.side = sd
	return sd, nil
}

// hashesAndSubjects lists the commits of a range as (hash, subject) pairs.
func (c *containsRun) hashesAndSubjects(rng string) ([][2]string, error) {
	out, err := c.m.run(c.ctx, "log", noSignature, hashAndSubject, rng, "--")
	if err != nil {
		return nil, err
	}
	var commits [][2]string
	for _, line := range strings.Split(out, "\n") {
		if hash, subject, ok := strings.Cut(strings.TrimRight(line, "\r"), "\x00"); ok {
			commits = append(commits, [2]string{hash, subject})
		}
	}
	return commits, nil
}

func (c *containsRun) stepAncestor() error {
	sd, err := c.targetSide()
	if err != nil {
		return err
	}
	inTarget := map[string]bool{}
	if sd != nil {
		inTarget = sd.hashes
	} else {
		out, err := c.m.run(c.ctx, "rev-list", c.fork+".."+c.target, "--")
		if err != nil {
			return err
		}
		for _, h := range strings.Fields(out) {
			inTarget[h] = true
		}
	}
	for _, i := range unmatched(c.per) {
		if inTarget[c.per[i].Hash] {
			c.per[i].By = byAncestor
		}
	}
	return nil
}

func (c *containsRun) stepPatch() error {
	out, err := c.m.run(c.ctx, "cherry", c.target, c.branch, c.fork)
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
	for _, i := range unmatched(c.per) {
		if equivalent[c.per[i].Hash] {
			c.per[i].By = byPatch
		}
	}
	return nil
}

// targetSubjects returns the subjects of branch..target.
func (c *containsRun) targetSubjects() ([]string, error) {
	sd, err := c.targetSide()
	if err != nil {
		return nil, err
	}
	if sd != nil {
		return sd.subjects, nil
	}
	commits, err := c.hashesAndSubjects(c.branch + ".." + c.target)
	if err != nil {
		return nil, err
	}
	subjects := make([]string, 0, len(commits))
	for _, hs := range commits {
		subjects = append(subjects, hs[1])
	}
	return subjects, nil
}

func (c *containsRun) stepSubject() error {
	subjects, err := c.targetSubjects()
	if err != nil {
		return err
	}
	// Each target commit vouches for at most one own commit.
	left := map[string]int{}
	for _, line := range subjects {
		if s := strings.TrimSpace(line); s != "" {
			left[s]++
		}
	}
	// A target commit that matched an own commit by its patch has vouched
	// already (a reworded pick only makes this stricter).
	for _, p := range c.per {
		if s := strings.TrimSpace(p.Subject); p.By == byPatch && left[s] > 0 {
			left[s]--
		}
	}
	for _, i := range unmatched(c.per) {
		if s := strings.TrimSpace(c.per[i].Subject); s != "" && left[s] > 0 {
			c.per[i].By = bySubject
			left[s]--
		}
	}
	return nil
}

// stepSquash looks for one target commit whose whole patch equals the
// branch's whole change. The candidates (target commits touching the
// branch's files) are listed first: usually there are none, and then the
// branch's own diff is not needed.
func (c *containsRun) stepSquash() error {
	names, err := c.m.run(c.ctx, "diff", "--name-only", "-z", "--no-ext-diff", "--no-color", "--no-textconv", c.fork, c.branch, "--")
	if err != nil || strings.Trim(names, "\x00") == "" {
		return err // no change at all: nothing a squash could hold
	}
	args := []string{"log", noSignature, "-p", "--full-diff", "--format=%H", "--no-ext-diff", "--no-color", "--no-textconv",
		"-n", strconv.Itoa(maxSquashCands), c.branch + ".." + c.target, "--"}
	files := strings.Split(strings.Trim(names, "\x00"), "\x00")
	if len(files) > maxPathspecFiles {
		files = files[:maxPathspecFiles]
	}
	for _, f := range files {
		args = append(args, literalPathPrefix+f)
	}
	patches, err := c.m.run(c.ctx, args...)
	if err != nil || strings.TrimSpace(patches) == "" {
		return err
	}
	diff, err := c.m.run(c.ctx, "diff", "--no-ext-diff", "--no-color", "--no-textconv", c.fork, c.branch, "--")
	if err != nil || strings.TrimSpace(diff) == "" {
		return err
	}
	whole, err := c.m.patchIDs(c.ctx, diff)
	if err != nil || len(whole) != 1 {
		return err
	}
	ids, err := c.m.patchIDs(c.ctx, patches)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if id == whole[0] {
			for _, i := range unmatched(c.per) {
				c.per[i].By = bySquash
			}
			return nil
		}
	}
	return nil
}

// patchIDs fingerprints every patch in input with patch-id --stable and
// returns the first field of each output line.
func (m *Matcher) patchIDs(ctx context.Context, input string) ([]string, error) {
	out, err := m.R.RunInput(ctx, m.Dir, []byte(input), "patch-id", "--stable")
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			ids = append(ids, f[0])
		}
	}
	return ids, nil
}

// Unpushed counts commits of branchRef that no remote-tracking ref has.
func (m *Matcher) Unpushed(ctx context.Context, branchRef string) (int, error) {
	out, err := m.run(ctx, "rev-list", "--count", branchRef, "--not", "--remotes", "--")
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(out))
}
