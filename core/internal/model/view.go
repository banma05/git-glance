package model

import (
	"context"
	"fmt"
	"sort"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/graph"
	"github.com/banma05/git-glance/core/internal/repo"
	"github.com/banma05/git-glance/core/internal/settings"
)

// Reasons a branch's folder cannot be deleted from disk, in the order they
// are checked.
const (
	blockNotInFolder   = "not-in-folder"
	blockMainFolder    = "main-folder"
	blockFolderMissing = "folder-missing"
	blockFolderLocked  = "locked-worktree"
	blockDirty         = "dirty"
	blockUnpushed      = "unpushed"
	blockNotMerged     = "not-merged"
	// blockError: the branch's details could not be read (BranchView.Error).
	blockError = "error"
)

// sourceUnknown is the assignment source of a branch without a trunk.
const sourceUnknown = "unknown"

// ReleaseSummary says which of the trunk and its release branches already
// hold every own commit of a branch (In) and which do not (NotIn).
type ReleaseSummary struct {
	State string   `json:"state"`
	In    []string `json:"in"`
	NotIn []string `json:"notIn"`
}

// BranchView is everything the panel shows about one feature branch. Dirty is
// set only when the branch's folder exists and has changes or new files.
// FolderHead is the commit the folder's HEAD was at ("" without a usable
// folder); a request to delete the folder quotes it back. FolderLocked is set
// by `git worktree lock`; FolderBroken (with FolderMissing) when the folder
// exists but git cannot read it. Branches whose trunk is unknown have no Own,
// fork, Test or Release.
type BranchView struct {
	Name            string            `json:"name"`
	Short           string            `json:"short"`
	Topic           string            `json:"topic"`
	Color           int               `json:"color"`
	Trunk           string            `json:"trunk"`
	TrunkSource     string            `json:"trunkSource"`
	Folder          string            `json:"folder"`
	FolderHead      string            `json:"folderHead"`
	MainFolder      bool              `json:"mainFolder"`
	FolderMissing   bool              `json:"folderMissing"`
	FolderBroken    bool              `json:"folderBroken"`
	FolderLocked    bool              `json:"folderLocked"`
	Dirty           *repo.DirtyCounts `json:"dirty"`
	HasUpstream     bool              `json:"hasUpstream"`
	UpstreamGone    bool              `json:"upstreamGone"`
	Ahead           int               `json:"ahead"`
	Behind          int               `json:"behind"`
	Unpushed        int               `json:"unpushed"`
	BehindTrunk     int               `json:"behindTrunk"`
	Own             []repo.CommitInfo `json:"own"`
	ForkHash        string            `json:"forkHash"`
	ForkTime        int64             `json:"forkTime"`
	Merged          bool              `json:"merged"`
	Test            *Containment      `json:"test"`
	Release         *ReleaseSummary   `json:"release"`
	CanRemoveFolder bool              `json:"canRemoveFolder"`
	RemoveBlock     string            `json:"removeBlock"`
	Old             bool              `json:"old"`
	LastCommit      int64             `json:"lastCommit"`
	Error           string            `json:"error"`
}

// TrunkView describes a trunk: the ref it is compared through, and how the
// local branch of the same name differs from that ref.
type TrunkView struct {
	Name        string   `json:"name"`
	Ref         string   `json:"ref"`
	Hash        string   `json:"hash"`
	Time        int64    `json:"time"`
	HasLocal    bool     `json:"hasLocal"`
	LocalAhead  int      `json:"localAhead"`
	LocalBehind int      `json:"localBehind"`
	Folder      string   `json:"folder"`
	Test        string   `json:"test"`
	Releases    []string `json:"releases"`
}

// Group is a trunk and its non-old branches, newest last commit first.
type Group struct {
	Trunk    TrunkView `json:"trunk"`
	Branches []string  `json:"branches"`
}

// View is the whole panel for one repository.
type View struct {
	Repo       string                `json:"repo"`
	Settings   settings.RepoSettings `json:"settings"`
	Groups     []Group               `json:"groups"`
	Unknown    []string              `json:"unknown"`
	Old        []string              `json:"old"`
	Branches   map[string]BranchView `json:"branches"`
	Sections   []graph.Section       `json:"sections"` // same order as Groups
	FetchedAt  int64                 `json:"fetchedAt"`
	FetchError string                `json:"fetchError"`
}

type builder struct {
	ctx      context.Context
	snap     repo.Snapshot
	rs       settings.RepoSettings
	now      int64
	trunks   []Trunk
	counts   map[string]map[string]AheadBehind // trunk name -> branch -> counts
	folders  map[string]repo.Worktree
	facts    gitFacts
	workers  int
	unpushed map[string]int // answered in one go; the others are asked per branch
}

// Build assembles the view of snap without keeping anything for the next
// build. See BuildCached.
func Build(ctx context.Context, r gitx.Runner, v gitx.Version, snap repo.Snapshot, rs settings.RepoSettings, now int64) (View, error) {
	return BuildCached(ctx, r, v, snap, rs, now, nil)
}

// BuildCached assembles the view of snap, keeping in c (which may be nil)
// what the next build of the same repository can reuse. Git is asked about
// the snapshot's commit hashes, never about short names, so a tag that
// shares a branch's name cannot shadow it and a ref that moves meanwhile
// cannot mix into the view. Branches are worked on by a few goroutines; the
// view does not depend on their order.
func BuildCached(ctx context.Context, r gitx.Runner, v gitx.Version, snap repo.Snapshot, rs settings.RepoSettings, now int64, c *Cache) (View, error) {
	return build(ctx, NewCounter(r, snap.Repo, v), newHashFacts(r, snap, c), buildWorkers, snap, rs, now)
}

func build(ctx context.Context, counter Counter, facts gitFacts, workers int, snap repo.Snapshot, rs settings.RepoSettings, now int64) (View, error) {
	rs = normalizeSettings(rs)
	b := &builder{
		ctx: ctx, snap: snap, rs: rs, now: now,
		trunks:  uniqueTrunks(ResolveTrunks(snap, rs)),
		folders: foldersByBranch(snap.Worktrees),
		facts:   facts, workers: workers,
	}
	features := featureRefs(snap.Local, rs, b.trunks)
	if err := b.count(counter, features); err != nil {
		return View{}, err
	}
	view := View{
		Repo: snap.Repo, Settings: rs,
		Groups: []Group{}, Unknown: []string{}, Old: []string{},
		Branches: map[string]BranchView{}, Sections: []graph.Section{},
	}
	b.unpushed = facts.unpushedAll(ctx, features)
	views := make([]BranchView, len(features))
	errs := forEach(len(features), workers, func(i int) error {
		var err error
		views[i], err = b.branch(features[i])
		return err
	})
	for i, ref := range features {
		bv := views[i]
		if err := errs[i]; err != nil {
			if ctx.Err() != nil {
				return View{}, fmt.Errorf("branch %s: %w", ref.Name, err)
			}
			// One unreadable branch must not hide all the others.
			bv = b.failedBranch(ref, err)
		}
		view.Branches[ref.Name] = bv
	}
	colorBranches(view.Branches)
	b.group(&view, features)
	sections, err := b.sections(view)
	if err != nil {
		return View{}, err
	}
	view.Sections = sections
	return view, nil
}

// normalizeSettings returns a copy whose slices and maps are never nil.
func normalizeSettings(rs settings.RepoSettings) settings.RepoSettings {
	trunks := make([]settings.TrunkConfig, 0, len(rs.Trunks))
	for _, t := range rs.Trunks {
		if t.Releases == nil {
			t.Releases = []string{}
		}
		trunks = append(trunks, t)
	}
	rs.Trunks = trunks
	if rs.ManualTrunk == nil {
		rs.ManualTrunk = map[string]string{}
	}
	if rs.CreatedBase == nil {
		rs.CreatedBase = map[string]string{}
	}
	return rs
}

// uniqueTrunks drops a trunk configured twice; a repeated name would tie
// with itself when assigning branches.
func uniqueTrunks(trunks []Trunk) []Trunk {
	seen := map[string]bool{}
	out := make([]Trunk, 0, len(trunks))
	for _, t := range trunks {
		if !seen[t.Name] {
			seen[t.Name] = true
			out = append(out, t)
		}
	}
	return out
}

func foldersByBranch(worktrees []repo.Worktree) map[string]repo.Worktree {
	folders := map[string]repo.Worktree{}
	for _, w := range worktrees {
		if w.Branch == "" || w.Detached {
			continue
		}
		if _, ok := folders[w.Branch]; !ok {
			folders[w.Branch] = w
		}
	}
	return folders
}

// featureRefs is every local branch that is not a trunk, test or release
// branch, whether or not that role resolved to an existing branch.
func featureRefs(local []repo.Ref, rs settings.RepoSettings, trunks []Trunk) []repo.Ref {
	roles := map[string]bool{}
	for _, c := range rs.Trunks {
		roles[c.Name], roles[c.Test] = true, true
		for _, n := range c.Releases {
			roles[n] = true
		}
	}
	for _, t := range trunks {
		roles[t.Name] = true
	}
	out := []repo.Ref{}
	for _, ref := range local {
		if !roles[ref.Name] {
			out = append(out, ref)
		}
	}
	return out
}

// fullRef names a resolved target unambiguously. resolveTarget gives a local
// branch Ref == Name and a remote-tracking one Ref == "<remote>/<name>".
func fullRef(t Target) string {
	if t.Ref == t.Name {
		return headRef(t.Ref)
	}
	return "refs/remotes/" + t.Ref
}

func headRef(branch string) string { return "refs/heads/" + branch }

// hasLocalCopy reports whether the trunk is compared through a remote ref
// while a local branch of the same name exists.
func (b *builder) hasLocalCopy(t Trunk) bool {
	_, ok := findRef(b.snap.Local, t.Name)
	return ok && t.Ref != t.Name
}

// count fills b.counts for every trunk: the feature branches, plus the local
// copy of the trunk itself when it is compared through a remote ref.
func (b *builder) count(c Counter, features []repo.Ref) error {
	b.counts = map[string]map[string]AheadBehind{}
	for _, t := range b.trunks {
		names := make([]string, 0, len(features)+1)
		for _, ref := range features {
			names = append(names, ref.Name)
		}
		if b.hasLocalCopy(t) {
			names = append(names, t.Name)
		}
		if len(names) == 0 {
			continue
		}
		res, err := c.Counts(b.ctx, names, fullRef(t.Target))
		if err != nil {
			return fmt.Errorf("counting against %s: %w", t.Name, err)
		}
		b.counts[t.Name] = res
	}
	return nil
}

func (b *builder) assign(branch string) Assignment {
	ahead := map[string]int{}
	order := make([]string, 0, len(b.trunks))
	for _, t := range b.trunks {
		order = append(order, t.Name)
		if c, ok := b.counts[t.Name][branch]; ok {
			ahead[t.Name] = c.Ahead
		}
	}
	return AssignTrunk(ahead, order, b.rs.CreatedBase[branch], b.rs.ManualTrunk[branch])
}

func (b *builder) trunk(name string) (Trunk, bool) {
	for _, t := range b.trunks {
		if t.Name == name {
			return t, true
		}
	}
	return Trunk{}, false
}

// baseView holds what the snapshot alone says about a branch.
func (b *builder) baseView(ref repo.Ref) BranchView {
	short := ShortName(ref.Name, b.rs.HidePrefix)
	bv := BranchView{
		Name: ref.Name, Short: short, Topic: Topic(short),
		HasUpstream: ref.Upstream != "", UpstreamGone: ref.UpstreamGone,
		Ahead: ref.Ahead, Behind: ref.Behind,
		Own: []repo.CommitInfo{}, LastCommit: ref.Time,
	}
	b.placeFolder(&bv)
	return bv
}

// failedBranch is the view of a branch whose details could not be read: what
// the snapshot says, with no trunk (so it shows among the unknown branches),
// never removable, and the error.
func (b *builder) failedBranch(ref repo.Ref, err error) BranchView {
	bv := b.baseView(ref)
	bv.TrunkSource = sourceUnknown
	bv.RemoveBlock = blockError
	bv.Error = err.Error()
	return bv
}

func (b *builder) branch(ref repo.Ref) (BranchView, error) {
	bv := b.baseView(ref)
	a := b.assign(ref.Name)
	bv.Trunk, bv.TrunkSource = a.Trunk, a.Source
	if t, ok := b.trunk(a.Trunk); ok {
		if err := b.compare(&bv, t, ref); err != nil {
			return bv, err
		}
	}
	n, ok := b.unpushed[ref.Name]
	if !ok {
		var err error
		if n, err = b.facts.unpushed(b.ctx, ref); err != nil {
			return bv, err
		}
	}
	bv.Unpushed = n
	bv.RemoveBlock = removeBlock(bv)
	bv.CanRemoveFolder = bv.RemoveBlock == ""
	bv.Old = IsOld(bv.Folder != "", bv.Merged, bv.LastCommit, b.now, b.rs.OldDays)
	return bv, nil
}

func (b *builder) placeFolder(bv *BranchView) {
	w, ok := b.folders[bv.Name]
	if !ok {
		return
	}
	bv.Folder, bv.MainFolder, bv.FolderMissing, bv.FolderLocked = w.Path, w.Main, w.Missing, w.Locked
	bv.FolderBroken = w.Broken
	if !w.Missing {
		bv.FolderHead = w.Head
	}
	if !w.Missing && w.Dirty.Changed+w.Dirty.Untracked > 0 {
		d := w.Dirty
		d.Files = append([]string{}, w.Dirty.Files...)
		bv.Dirty = &d
	}
}

// compare fills the fields that need the branch's trunk. A branch that shares
// no history with its trunk is moved back to unknown.
func (b *builder) compare(bv *BranchView, t Trunk, ref repo.Ref) error {
	o, err := b.facts.ownCommits(b.ctx, t.Target, ref)
	if err != nil {
		return err
	}
	if o.noFork {
		bv.Trunk, bv.TrunkSource = "", sourceUnknown
		return nil
	}
	bv.Own, bv.ForkHash, bv.ForkTime = o.own, o.fork, o.forkTime
	bv.BehindTrunk = b.counts[t.Name][bv.Name].Behind
	in := func(target Target) (Containment, error) {
		return b.facts.contains(b.ctx, target, ref, o)
	}
	onTrunk, err := in(t.Target)
	if err != nil {
		return err
	}
	bv.Merged = onTrunk.State == stateAll
	if t.Test != nil {
		c, err := in(*t.Test)
		if err != nil {
			return err
		}
		bv.Test = &c
	}
	rel := &ReleaseSummary{In: []string{}, NotIn: []string{}}
	rel.add(t.Name, onTrunk)
	for _, target := range t.Releases {
		c, err := in(target)
		if err != nil {
			return err
		}
		rel.add(target.Name, c)
	}
	rel.State = rel.state()
	bv.Release = rel
	return nil
}

func (s *ReleaseSummary) add(name string, c Containment) {
	if c.State == stateAll {
		s.In = append(s.In, name)
	} else {
		s.NotIn = append(s.NotIn, name)
	}
}

func (s *ReleaseSummary) state() string {
	switch {
	case len(s.NotIn) == 0:
		return stateAll
	case len(s.In) == 0:
		return stateNone
	}
	return stateSome
}

// removeBlock returns the first reason the branch's folder cannot be
// deleted from disk, or "" when it can.
func removeBlock(bv BranchView) string {
	switch {
	case bv.Folder == "":
		return blockNotInFolder
	case bv.MainFolder:
		return blockMainFolder
	case bv.FolderMissing:
		return blockFolderMissing
	case bv.FolderLocked:
		return blockFolderLocked
	case bv.Dirty != nil:
		return blockDirty
	case bv.Unpushed > 0:
		return blockUnpushed
	case !bv.Merged:
		return blockNotMerged
	}
	return ""
}

// colorBranches colours topics of non-old branches; an old branch takes the
// colour of a non-old branch with the same topic, else slot 0.
func colorBranches(branches map[string]BranchView) {
	topics := []string{}
	for _, bv := range branches {
		if !bv.Old {
			topics = append(topics, bv.Topic)
		}
	}
	colors := AssignColors(topics)
	for name, bv := range branches {
		bv.Color = colors[bv.Topic]
		branches[name] = bv
	}
}

// group sorts feature branches into trunk groups, Unknown and Old, each
// newest last commit first (then by name).
func (b *builder) group(view *View, features []repo.Ref) {
	index := map[string]int{}
	for i, t := range b.trunks {
		index[t.Name] = i
		view.Groups = append(view.Groups, Group{Trunk: b.trunkView(t), Branches: []string{}})
	}
	for _, ref := range features {
		bv := view.Branches[ref.Name]
		switch {
		case bv.Old:
			view.Old = append(view.Old, bv.Name)
		case bv.Trunk == "":
			view.Unknown = append(view.Unknown, bv.Name)
		default:
			g := &view.Groups[index[bv.Trunk]]
			g.Branches = append(g.Branches, bv.Name)
		}
	}
	byNewest := func(names []string) {
		sort.SliceStable(names, func(i, j int) bool {
			x, y := view.Branches[names[i]], view.Branches[names[j]]
			if x.LastCommit != y.LastCommit {
				return x.LastCommit > y.LastCommit
			}
			return x.Name < y.Name
		})
	}
	for i := range view.Groups {
		byNewest(view.Groups[i].Branches)
	}
	byNewest(view.Unknown)
	byNewest(view.Old)
}

func (b *builder) trunkView(t Trunk) TrunkView {
	tv := TrunkView{Name: t.Name, Ref: t.Ref, Hash: t.Hash, Time: t.Time, Releases: []string{}}
	_, tv.HasLocal = findRef(b.snap.Local, t.Name)
	if c, ok := b.counts[t.Name][t.Name]; ok {
		tv.LocalAhead, tv.LocalBehind = c.Ahead, c.Behind
	}
	if w, ok := b.folders[t.Name]; ok {
		tv.Folder = w.Path
	}
	if t.Test != nil {
		tv.Test = t.Test.Name
	}
	for _, r := range t.Releases {
		tv.Releases = append(tv.Releases, r.Name)
	}
	return tv
}

func section(t Trunk, names []string, branches map[string]BranchView, between graph.Between) (graph.Section, error) {
	in := make([]graph.BranchInput, 0, len(names))
	for _, n := range names {
		bv := branches[n]
		in = append(in, graph.BranchInput{
			Name: n, Own: bv.Own, ForkHash: bv.ForkHash, ForkTime: bv.ForkTime,
			Merged: bv.Merged, LastCommit: bv.LastCommit,
		})
	}
	return graph.Layout(graph.TrunkInput{Name: t.Name, HeadHash: t.Hash, HeadTime: t.Time}, in, between)
}

type anchorPair struct{ newer, older string }

// sections lays out the graph of every group. Which trunk counts a layout
// asks for does not depend on the answers, so a first pass only collects the
// pairs, the workers count them, and a second pass lays out with the counts.
func (b *builder) sections(view View) ([]graph.Section, error) {
	var pairs []anchorPair
	group := map[anchorPair]int{} // first group that needs the pair
	for i, g := range view.Groups {
		collect := func(newer, older string) (int, error) {
			p := anchorPair{newer, older}
			if _, ok := group[p]; !ok {
				group[p] = i
				pairs = append(pairs, p)
			}
			return 0, nil
		}
		if _, err := section(b.trunks[i], g.Branches, view.Branches, collect); err != nil {
			return nil, fmt.Errorf("graph of %s: %w", g.Trunk.Name, err)
		}
	}
	counts := make([]int, len(pairs))
	errs := forEach(len(pairs), b.workers, func(i int) error {
		var err error
		counts[i], err = b.facts.between(b.ctx, pairs[i].newer, pairs[i].older)
		return err
	})
	known := map[anchorPair]int{}
	for i, p := range pairs {
		if errs[i] != nil {
			return nil, fmt.Errorf("graph of %s: %w", view.Groups[group[p]].Trunk.Name, errs[i])
		}
		known[p] = counts[i]
	}
	lookup := func(newer, older string) (int, error) {
		n, ok := known[anchorPair{newer, older}]
		if !ok {
			return 0, fmt.Errorf("no count between %s and %s", older, newer)
		}
		return n, nil
	}
	out := make([]graph.Section, 0, len(view.Groups))
	for i, g := range view.Groups {
		s, err := section(b.trunks[i], g.Branches, view.Branches, lookup)
		if err != nil {
			return nil, fmt.Errorf("graph of %s: %w", g.Trunk.Name, err)
		}
		out = append(out, s)
	}
	return out, nil
}
