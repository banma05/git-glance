package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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
}

// hashFacts asks git about the commit hashes of the snapshot, never about
// ref names: an answer then depends on nothing but those hashes, so it can be
// kept in the Cache, and a ref that moved since the snapshot cannot mix into
// the view.
type hashFacts struct {
	r       gitx.Runner
	dir     string
	m       *Matcher
	cache   *Cache
	remotes string // fingerprint of the snapshot's remote-tracking refs
}

func newHashFacts(r gitx.Runner, snap repo.Snapshot, c *Cache) *hashFacts {
	return &hashFacts{r: r, dir: snap.Repo, m: &Matcher{R: r, Dir: snap.Repo}, cache: c, remotes: remotesKey(snap.Remote)}
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
	if v, ok := f.cache.get(key); ok {
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
	f.cache.put(key, o.clone())
	return o, nil
}

func (f *hashFacts) contains(ctx context.Context, target Target, ref repo.Ref, o ownResult) (Containment, error) {
	if err := needHashes(target.Hash, ref.Hash, o.fork); err != nil {
		return Containment{}, err
	}
	// own is fixed by the fork and the branch tip, so these three name every
	// input.
	key := "contains\x00" + target.Hash + "\x00" + ref.Hash + "\x00" + o.fork
	if v, ok := f.cache.get(key); ok {
		return cloneContainment(v.(Containment)), nil
	}
	c, err := f.m.Contains(ctx, target.Hash, ref.Hash, o.fork, o.own)
	if err != nil {
		return Containment{}, err
	}
	f.cache.put(key, cloneContainment(c))
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
		if v, ok := f.cache.get(f.unpushedKey(ref.Hash)); ok {
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
			f.cache.put(f.unpushedKey(ref.Hash), n)
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
	if v, ok := f.cache.get(key); ok {
		return v.(int), nil
	}
	n, err := countBetween(ctx, f.r, f.dir, newer, older)
	if err == nil {
		f.cache.put(key, n)
	}
	return n, err
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

// forEach runs fn for 0..n-1 on at most workers goroutines and returns each
// call's error. A panic becomes that call's error.
func forEach(n, workers int, fn func(i int) error) []error {
	errs := make([]error, n)
	next := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < min(workers, n); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				errs[i] = recovered(func() error { return fn(i) })
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

func recovered(fn func() error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	return fn()
}
