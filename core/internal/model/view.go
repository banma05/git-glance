package model

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/graph"
	"github.com/banma05/git-glance/core/internal/repo"
	"github.com/banma05/git-glance/core/internal/settings"
)

// Reasons a branch cannot be collapsed, in the order they are checked.
const (
	blockNotInFolder   = "not-in-folder"
	blockMainFolder    = "main-folder"
	blockFolderMissing = "folder-missing"
	blockDirty         = "dirty"
	blockUnpushed      = "unpushed"
	blockNotMerged     = "not-merged"
	sourceUnknown      = "unknown"
)

// ReleaseSummary says which of the trunk and its release branches already
// hold every own commit of a branch (In) and which do not (NotIn).
type ReleaseSummary struct {
	State string   `json:"state"`
	In    []string `json:"in"`
	NotIn []string `json:"notIn"`
}

// BranchView is everything the panel shows about one feature branch. Dirty is
// set only when the branch's folder exists and has changes or new files.
// Branches whose trunk is unknown have no Own, fork, Test or Release.
type BranchView struct {
	Name          string            `json:"name"`
	Short         string            `json:"short"`
	Topic         string            `json:"topic"`
	Color         int               `json:"color"`
	Trunk         string            `json:"trunk"`
	TrunkSource   string            `json:"trunkSource"`
	Folder        string            `json:"folder"`
	MainFolder    bool              `json:"mainFolder"`
	FolderMissing bool              `json:"folderMissing"`
	Dirty         *repo.DirtyCounts `json:"dirty"`
	HasUpstream   bool              `json:"hasUpstream"`
	UpstreamGone  bool              `json:"upstreamGone"`
	Ahead         int               `json:"ahead"`
	Behind        int               `json:"behind"`
	Unpushed      int               `json:"unpushed"`
	BehindTrunk   int               `json:"behindTrunk"`
	Own           []repo.CommitInfo `json:"own"`
	ForkHash      string            `json:"forkHash"`
	ForkTime      int64             `json:"forkTime"`
	Merged        bool              `json:"merged"`
	Test          *Containment      `json:"test"`
	Release       *ReleaseSummary   `json:"release"`
	CanCollapse   bool              `json:"canCollapse"`
	CollapseBlock string            `json:"collapseBlock"`
	Old           bool              `json:"old"`
	LastCommit    int64             `json:"lastCommit"`
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
	ctx     context.Context
	r       gitx.Runner
	dir     string
	snap    repo.Snapshot
	rs      settings.RepoSettings
	now     int64
	trunks  []Trunk
	counts  map[string]map[string]AheadBehind // trunk name -> branch -> counts
	folders map[string]repo.Worktree
	m       *Matcher
}

// Build assembles the view of snap. Every git call uses full ref names, so a
// tag that shares a branch's name cannot shadow it.
func Build(ctx context.Context, r gitx.Runner, v gitx.Version, snap repo.Snapshot, rs settings.RepoSettings, now int64) (View, error) {
	rs = normalizeSettings(rs)
	b := &builder{
		ctx: ctx, r: r, dir: snap.Repo, snap: snap, rs: rs, now: now,
		trunks:  uniqueTrunks(ResolveTrunks(snap, rs)),
		folders: foldersByBranch(snap.Worktrees),
		m:       &Matcher{R: r, Dir: snap.Repo},
	}
	features := featureRefs(snap.Local, rs, b.trunks)
	if err := b.count(NewCounter(r, snap.Repo, v), features); err != nil {
		return View{}, err
	}
	view := View{
		Repo: snap.Repo, Settings: rs,
		Groups: []Group{}, Unknown: []string{}, Old: []string{},
		Branches: map[string]BranchView{}, Sections: []graph.Section{},
	}
	for _, ref := range features {
		bv, err := b.branch(ref)
		if err != nil {
			return View{}, fmt.Errorf("branch %s: %w", ref.Name, err)
		}
		view.Branches[ref.Name] = bv
	}
	colorBranches(view.Branches)
	b.group(&view, features)
	for i, g := range view.Groups {
		s, err := b.section(b.trunks[i], g.Branches, view.Branches)
		if err != nil {
			return View{}, fmt.Errorf("graph of %s: %w", g.Trunk.Name, err)
		}
		view.Sections = append(view.Sections, s)
	}
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

func (b *builder) branch(ref repo.Ref) (BranchView, error) {
	short := ShortName(ref.Name, b.rs.HidePrefix)
	bv := BranchView{
		Name: ref.Name, Short: short, Topic: Topic(short),
		HasUpstream: ref.Upstream != "", UpstreamGone: ref.UpstreamGone,
		Ahead: ref.Ahead, Behind: ref.Behind,
		Own: []repo.CommitInfo{}, LastCommit: ref.Time,
	}
	b.placeFolder(&bv)
	a := b.assign(ref.Name)
	bv.Trunk, bv.TrunkSource = a.Trunk, a.Source
	branchRef := headRef(ref.Name)
	if t, ok := b.trunk(a.Trunk); ok {
		if err := b.compare(&bv, t, branchRef); err != nil {
			return bv, err
		}
	}
	n, err := b.m.Unpushed(b.ctx, branchRef)
	if err != nil {
		return bv, err
	}
	bv.Unpushed = n
	bv.CollapseBlock = collapseBlock(bv)
	bv.CanCollapse = bv.CollapseBlock == ""
	bv.Old = IsOld(bv.Folder != "", bv.Merged, bv.LastCommit, b.now, b.rs.OldDays)
	return bv, nil
}

func (b *builder) placeFolder(bv *BranchView) {
	w, ok := b.folders[bv.Name]
	if !ok {
		return
	}
	bv.Folder, bv.MainFolder, bv.FolderMissing = w.Path, w.Main, w.Missing
	if !w.Missing && w.Dirty.Changed+w.Dirty.Untracked > 0 {
		d := w.Dirty
		d.Files = append([]string{}, w.Dirty.Files...)
		bv.Dirty = &d
	}
}

// compare fills the fields that need the branch's trunk. A branch that shares
// no history with its trunk is moved back to unknown.
func (b *builder) compare(bv *BranchView, t Trunk, branchRef string) error {
	own, fork, forkTime, err := b.m.OwnCommits(b.ctx, fullRef(t.Target), branchRef)
	if errors.Is(err, errNoForkPoint) {
		bv.Trunk, bv.TrunkSource = "", sourceUnknown
		return nil
	}
	if err != nil {
		return err
	}
	bv.Own, bv.ForkHash, bv.ForkTime = own, fork, forkTime
	bv.BehindTrunk = b.counts[t.Name][bv.Name].Behind
	in := func(target Target) (Containment, error) {
		return b.m.Contains(b.ctx, fullRef(target), branchRef, fork, own)
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

// collapseBlock returns the first reason the branch's folder cannot be
// collapsed, or "" when it can.
func collapseBlock(bv BranchView) string {
	switch {
	case bv.Folder == "":
		return blockNotInFolder
	case bv.MainFolder:
		return blockMainFolder
	case bv.FolderMissing:
		return blockFolderMissing
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

func (b *builder) section(t Trunk, names []string, branches map[string]BranchView) (graph.Section, error) {
	in := make([]graph.BranchInput, 0, len(names))
	for _, n := range names {
		bv := branches[n]
		in = append(in, graph.BranchInput{
			Name: n, Own: bv.Own, ForkHash: bv.ForkHash, ForkTime: bv.ForkTime,
			Merged: bv.Merged, LastCommit: bv.LastCommit,
		})
	}
	return graph.Layout(graph.TrunkInput{Name: t.Name, HeadHash: t.Hash, HeadTime: t.Time}, in, b.between)
}

// between counts trunk commits strictly between two anchors:
// rev-list --count <older>..<newer>, minus the newer anchor itself.
func (b *builder) between(newer, older string) (int, error) {
	out, err := b.r.Run(b.ctx, b.dir, "rev-list", "--count", older+".."+newer, "--")
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("unexpected count %q", out)
	}
	if n--; n < 0 {
		n = 0
	}
	return n, nil
}
