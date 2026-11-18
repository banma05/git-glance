package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/repo"
)

// buildWorkers is how many branches a build works on at the same time.
const buildWorkers = 4

// maxTipArgs caps the commits named on one git command line.
const maxTipArgs = 200

// ownResult is what OwnCommits says about a branch and its trunk.
type ownResult struct {
	own      []repo.CommitInfo
	fork     string
	forkTime int64
	noFork   bool // the branch shares no history with the trunk
}

func (o ownResult) clone() ownResult {
	if o.own != nil {
		o.own = append([]repo.CommitInfo{}, o.own...)
	}
	return o
}

func cloneContainment(c Containment) Containment {
	c.Per = append([]CommitMatch{}, c.Per...)
	return c
}

// gitFacts answers the git questions of a build.
type gitFacts interface {
	ownCommits(ctx context.Context, trunk Target, ref repo.Ref) (ownResult, error)
	contains(ctx context.Context, target Target, ref repo.Ref, o ownResult) (Containment, error)
	// unpushedAll answers for as many branches as it can at once; the others
	// are asked one by one through unpushed.
	unpushedAll(ctx context.Context, refs []repo.Ref) map[string]int
	unpushed(ctx context.Context, ref repo.Ref) (int, error)
	between(ctx context.Context, newer, older string) (int, error)
	// aheadBehindAll counts, for git without %(ahead-behind:), as many
	// (trunk tip, branch tip) pairs as it can at once; the others are asked
	// one by one through aheadBehind.
	aheadBehindAll(ctx context.Context, pairs []tipPair) map[tipPair]AheadBehind
	aheadBehind(ctx context.Context, trunk Target, ref repo.Ref) (AheadBehind, error)
}

// tipPair is a trunk tip and a branch tip.
type tipPair struct{ trunk, tip string }

// hashFacts asks git about the commit hashes of the snapshot, never about
// ref names: an answer then depends on nothing but those hashes and the
// history git shows for them (the shallow boundary, grafts and replace refs,
// fingerprinted in history and part of every cache key), so it can be kept in
// the Cache, and a ref that moved since the snapshot cannot mix into the
// answers.
type hashFacts struct {
	r       gitx.Runner
	dir     string
	m       *Matcher
	cache   *Cache
	history string // fingerprint of what can change the history behind a hash
	remotes string // fingerprint of the snapshot's remote-tracking refs
}

// newHashFacts prepares the facts of one build. When the history
// fingerprint cannot be read, nothing is cached.
func newHashFacts(ctx context.Context, r gitx.Runner, snap repo.Snapshot, c *Cache) *hashFacts {
	f := &hashFacts{r: r, dir: snap.Repo, m: &Matcher{R: r, Dir: snap.Repo}, cache: c, remotes: remotesKey(snap.Remote)}
	if c != nil {
		history, err := historyKey(ctx, r, snap.Repo)
		if err != nil {
			f.cache = nil
		}
		f.history = history
	}
	return f
}

func (f *hashFacts) get(key string) (any, bool) { return f.cache.get(f.history + "\x00" + key) }
func (f *hashFacts) put(key string, v any)      { f.cache.put(f.history+"\x00"+key, v) }

// historyKey fingerprints what can make the same commit hashes stand for a
// different history: the shallow boundary, grafts and replace refs.
func historyKey(ctx context.Context, r gitx.Runner, dir string) (string, error) {
	out, err := r.Run(ctx, dir, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	common := strings.TrimSpace(string(out))
	if !filepath.IsAbs(common) {
		common = filepath.Join(dir, common)
	}
	h := sha256.New()
	for _, name := range []string{"shallow", "info/grafts"} {
		data, err := os.ReadFile(filepath.Join(common, filepath.FromSlash(name)))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		fmt.Fprintf(h, "%s %d\n", name, len(data))
		h.Write(data)
	}
	out, err = r.Run(ctx, dir, "for-each-ref", "--format=%(refname) %(objectname)", "refs/replace")
	if err != nil {
		return "", err
	}
	h.Write(out)
	return hex.EncodeToString(h.Sum(nil)), nil
}

func needHashes(hashes ...string) error {
	for _, h := range hashes {
		if !isHash(h) {
			return fmt.Errorf("no commit hash in the snapshot (%q)", h)
		}
	}
	return nil
}

func (f *hashFacts) ownCommits(ctx context.Context, trunk Target, ref repo.Ref) (ownResult, error) {
	if err := needHashes(trunk.Hash, ref.Hash); err != nil {
		return ownResult{}, err
	}
	key := "own\x00" + trunk.Hash + "\x00" + ref.Hash
	if v, ok := f.get(key); ok {
		return v.(ownResult).clone(), nil
	}
	own, fork, forkTime, err := f.m.OwnCommits(ctx, trunk.Hash, ref.Hash)
	o := ownResult{own: own, fork: fork, forkTime: forkTime}
	if errors.Is(err, errNoForkPoint) {
		o, err = ownResult{noFork: true}, nil
	}
	if err != nil {
		return ownResult{}, err
	}
	f.put(key, o.clone())
	return o, nil
}

func (f *hashFacts) contains(ctx context.Context, target Target, ref repo.Ref, o ownResult) (Containment, error) {
	if err := needHashes(target.Hash, ref.Hash, o.fork); err != nil {
		return Containment{}, err
	}
	// own is fixed by the fork and the branch tip, so these three name every
	// input.
	key := "contains\x00" + target.Hash + "\x00" + ref.Hash + "\x00" + o.fork
	if v, ok := f.get(key); ok {
		return cloneContainment(v.(Containment)), nil
	}
	c, err := f.m.Contains(ctx, target.Hash, ref.Hash, o.fork, o.own)
	if err != nil {
		return Containment{}, err
	}
	f.put(key, cloneContainment(c))
	return c, nil
}

func (f *hashFacts) unpushedKey(tip string) string { return "unpushed\x00" + tip + "\x00" + f.remotes }

func (f *hashFacts) unpushedAll(ctx context.Context, refs []repo.Ref) map[string]int {
	res := map[string]int{}
	var missing []string
	for _, ref := range refs {
		if !isHash(ref.Hash) {
			continue
		}
		if v, ok := f.get(f.unpushedKey(ref.Hash)); ok {
			res[ref.Name] = v.(int)
		} else if !slices.Contains(missing, ref.Hash) {
			missing = append(missing, ref.Hash)
		}
	}
	if len(missing) == 0 {
		return res
	}
	counts, err := countUnpushed(ctx, f.r, f.dir, missing)
	if err != nil {
		return res // the rest are asked one by one
	}
	// The counts used the remote-tracking refs of now; they are kept under
	// the snapshot's fingerprint only when those are still the same refs.
	keep := f.remotesUnchanged(ctx)
	for _, ref := range refs {
		n, ok := counts[ref.Hash]
		if _, done := res[ref.Name]; done || !ok {
			continue
		}
		res[ref.Name] = n
		if keep {
			f.put(f.unpushedKey(ref.Hash), n)
		}
	}
	return res
}

func (f *hashFacts) unpushed(ctx context.Context, ref repo.Ref) (int, error) {
	if err := needHashes(ref.Hash); err != nil {
		return 0, err
	}
	return f.m.Unpushed(ctx, ref.Hash)
}

func (f *hashFacts) between(ctx context.Context, newer, older string) (int, error) {
	if err := needHashes(newer, older); err != nil {
		return 0, err
	}
	key := "between\x00" + newer + "\x00" + older
	if v, ok := f.get(key); ok {
		return v.(int), nil
	}
	n, err := countBetween(ctx, f.r, f.dir, newer, older)
	if err == nil {
		f.put(key, n)
	}
	return n, err
}

func aheadBehindKey(p tipPair) string { return "aheadBehind\x00" + p.trunk + "\x00" + p.tip }

func (f *hashFacts) aheadBehindAll(ctx context.Context, pairs []tipPair) map[tipPair]AheadBehind {
	res := map[tipPair]AheadBehind{}
	var missing []tipPair
	for _, p := range pairs {
		if !isHash(p.trunk) || !isHash(p.tip) {
			continue
		}
		if v, ok := f.get(aheadBehindKey(p)); ok {
			res[p] = v.(AheadBehind)
		} else if _, dup := res[p]; !dup && !slices.Contains(missing, p) {
			missing = append(missing, p)
		}
	}
	if len(missing) == 0 {
		return res
	}
	counts, err := countAheadBehind(ctx, f.r, f.dir, missing)
	if err != nil {
		return res // the rest are asked one by one
	}
	for p, ab := range counts {
		res[p] = ab
		f.put(aheadBehindKey(p), ab)
	}
	return res
}

func (f *hashFacts) aheadBehind(ctx context.Context, trunk Target, ref repo.Ref) (AheadBehind, error) {
	if err := needHashes(trunk.Hash, ref.Hash); err != nil {
		return AheadBehind{}, err
	}
	key := aheadBehindKey(tipPair{trunk.Hash, ref.Hash})
	if v, ok := f.get(key); ok {
		return v.(AheadBehind), nil
	}
	out, err := f.r.Run(ctx, f.dir, "rev-list", "--left-right", "--count", trunk.Hash+"..."+ref.Hash, "--")
	if err != nil {
		return AheadBehind{}, err
	}
	ab, err := parseCounts(string(out), true)
	if err != nil {
		return AheadBehind{}, err
	}
	f.put(key, ab)
	return ab, nil
}

// remotesUnchanged reports whether the remote-tracking refs are still those
// of the snapshot.
func (f *hashFacts) remotesUnchanged(ctx context.Context) bool {
	out, err := f.r.Run(ctx, f.dir, "for-each-ref", "--format=%(refname)%00%(objectname)", "refs/remotes")
	if err != nil {
		return false
	}
	var refs []repo.Ref
	for _, line := range strings.Split(string(out), "\n") {
		name, hash, ok := strings.Cut(strings.TrimRight(line, "\r"), "\x00")
		name = strings.TrimPrefix(name, "refs/remotes/")
		if ok && !strings.HasSuffix(name, "/HEAD") {
			refs = append(refs, repo.Ref{Name: name, Hash: hash})
		}
	}
	return remotesKey(refs) == f.remotes
}

// remotesKey fingerprints a set of remote-tracking refs (names and hashes),
// in the form repo.Read lists them.
func remotesKey(refs []repo.Ref) string {
	lines := make([]string, 0, len(refs))
	for _, r := range refs {
		lines = append(lines, r.Name+"\x00"+r.Hash)
	}
	slices.Sort(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

// countUnpushed counts, for every tip, the commits it reaches that no
// remote-tracking ref reaches. One `rev-list --parents <tips> --not --remotes`
// lists exactly the commits some tip reaches and no remote does, with their
// parents. A tip's count is what it reaches inside that list: a commit on
// the way that a remote reached would make everything below it reachable
// from that remote too, so no path leaves the list. A tip that is not listed
// is reached by a remote and counts 0.
func countUnpushed(ctx context.Context, r gitx.Runner, dir string, tips []string) (map[string]int, error) {
	res := map[string]int{}
	for start := 0; start < len(tips); start += maxTipArgs {
		chunk := tips[start:min(start+maxTipArgs, len(tips))]
		args := append(append([]string{"rev-list", "--parents"}, chunk...), "--not", "--remotes", "--")
		out, err := r.Run(ctx, dir, args...)
		if err != nil {
			return nil, err
		}
		parents := map[string][]string{}
		for _, line := range strings.Split(string(out), "\n") {
			f := strings.Fields(line)
			if len(f) == 0 {
				continue
			}
			if !isHash(f[0]) {
				return nil, fmt.Errorf("unexpected rev-list line %q", line)
			}
			parents[f[0]] = f[1:]
		}
		for _, tip := range chunk {
			res[tip] = reachedWithin(parents, tip)
		}
	}
	return res, nil
}

// reachedWithin counts the commits of the graph that start reaches.
func reachedWithin(parents map[string][]string, start string) int {
	if _, ok := parents[start]; !ok {
		return 0
	}
	seen := map[string]bool{start: true}
	stack := []string{start}
	for len(stack) > 0 {
		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, p := range parents[c] {
			if _, in := parents[p]; in && !seen[p] {
				seen[p] = true
				stack = append(stack, p)
			}
		}
	}
	return len(seen)
}

// countBetween counts trunk commits strictly between two anchors:
// rev-list --count <older>..<newer>, minus the newer anchor itself.
func countBetween(ctx context.Context, r gitx.Runner, dir, newer, older string) (int, error) {
	out, err := r.Run(ctx, dir, "rev-list", "--count", older+".."+newer, "--")
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("unexpected count %q", out)
	}
	return max(n-1, 0), nil
}

// countAheadBehind counts every pair from one walk of their history, what
// `rev-list --left-right --count trunk...tip` counts: ahead is what the tip
// reaches and the trunk does not, behind the other way round.
// `rev-list --parents <tips…>` lists every commit any of them reaches, with
// its parents, so the reachable sets can be walked in-process. Tips go in
// chunks, each with its trunks, to keep the command line short.
func countAheadBehind(ctx context.Context, r gitx.Runner, dir string, pairs []tipPair) (map[tipPair]AheadBehind, error) {
	byTrunk := map[string][]string{}
	var trunks []string
	for _, p := range pairs {
		if _, ok := byTrunk[p.trunk]; !ok {
			trunks = append(trunks, p.trunk)
		}
		byTrunk[p.trunk] = append(byTrunk[p.trunk], p.tip)
	}
	res := map[tipPair]AheadBehind{}
	for _, trunk := range trunks {
		tips := byTrunk[trunk]
		for start := 0; start < len(tips); start += maxTipArgs - 1 {
			chunk := tips[start:min(start+maxTipArgs-1, len(tips))]
			args := append([]string{"rev-list", "--parents", trunk}, chunk...)
			out, err := r.Run(ctx, dir, append(args, "--")...)
			if err != nil {
				return nil, err
			}
			g, err := readGraph(string(out))
			if err != nil {
				return nil, err
			}
			inTrunk, err := g.reach(trunk)
			if err != nil {
				return nil, err
			}
			trunkSize := 0
			for _, in := range inTrunk {
				if in {
					trunkSize++
				}
			}
			for _, tip := range chunk {
				reached, err := g.reach(tip)
				if err != nil {
					return nil, err
				}
				ab := AheadBehind{Behind: trunkSize}
				for i, in := range reached {
					switch {
					case in && inTrunk[i]:
						ab.Behind--
					case in:
						ab.Ahead++
					}
				}
				res[tipPair{trunk, tip}] = ab
			}
		}
	}
	return res, nil
}

// commitGraph is a commit history read from `rev-list --parents`.
type commitGraph struct {
	index   map[string]int32
	parents [][]int32
}

func readGraph(out string) (*commitGraph, error) {
	g := &commitGraph{index: map[string]int32{}}
	id := func(h string) int32 {
		i, ok := g.index[h]
		if !ok {
			i = int32(len(g.parents))
			g.index[h] = i
			g.parents = append(g.parents, nil)
		}
		return i
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		for _, h := range f {
			if !isHash(h) {
				return nil, fmt.Errorf("unexpected rev-list line %q", line)
			}
		}
		c := id(f[0])
		ps := make([]int32, 0, len(f)-1)
		for _, h := range f[1:] {
			ps = append(ps, id(h))
		}
		g.parents[c] = ps
	}
	return g, nil
}

// reach marks every commit the commit from reaches, itself included. rev-list
// lists every commit it started from, so a missing one is an error.
func (g *commitGraph) reach(from string) ([]bool, error) {
	start, ok := g.index[from]
	if !ok {
		return nil, fmt.Errorf("%s is not in the listed history", from)
	}
	seen := make([]bool, len(g.parents))
	stack := []int32{start}
	seen[start] = true
	for len(stack) > 0 {
		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, p := range g.parents[c] {
			if !seen[p] {
				seen[p] = true
				stack = append(stack, p)
			}
		}
	}
	return seen, nil
}

// isHash reports whether s is a full SHA-1 or SHA-256 object name.
func isHash(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f') {
			return false
		}
	}
	return true
}

// buildSlots is shared by every build in the process: together they run at
// most buildWorkers calls of forEach at a time, however many views are built
// at once.
var buildSlots = make(chan struct{}, buildWorkers)

// forEach runs fn for 0..n-1 on at most workers goroutines and returns each
// call's error. Each call first takes one of the process-wide buildSlots; if
// ctx ends while it waits, the call's error is ctx's. A panic becomes that
// call's error.
func forEach(ctx context.Context, n, workers int, fn func(i int) error) []error {
	errs := make([]error, n)
	next := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < max(1, min(workers, n)); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				errs[i] = inSlot(ctx, func() error { return recovered(func() error { return fn(i) }) })
			}
		}()
	}
	for i := 0; i < n; i++ {
		next <- i
	}
	close(next)
	wg.Wait()
	return errs
}

// inSlot runs fn holding one of buildSlots.
func inSlot(ctx context.Context, fn func() error) error {
	select {
	case buildSlots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-buildSlots }()
	return fn()
}

func recovered(fn func() error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	return fn()
}
