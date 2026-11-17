// Package graph lays out a trunk and its branches as compressed display rows.
package graph

import (
	"sort"

	"github.com/banma05/git-glance/core/internal/repo"
)

// Row is one display row of the branch graph.
type Row struct {
	Kind   string `json:"kind"` // head|commit|more-own|fork|merge|more
	Lane   int    `json:"lane"`
	Branch string `json:"branch"`
	Hash   string `json:"hash"`
	Time   int64  `json:"time"`
	Count  int    `json:"count"`
	Ends   []int  `json:"ends"` // fork rows: lanes that join the trunk here
}

// Section is the laid-out graph for one trunk.
type Section struct {
	Trunk string `json:"trunk"`
	Rows  []Row  `json:"rows"`
	Lanes int    `json:"lanes"`
}

// TrunkInput describes the trunk tip.
type TrunkInput struct {
	Name     string
	HeadHash string
	HeadTime int64
}

// BranchInput describes one branch; Own is newest first.
type BranchInput struct {
	Name       string
	Own        []repo.CommitInfo
	ForkHash   string
	ForkTime   int64
	Merged     bool
	LastCommit int64
}

// Between returns the number of trunk commits strictly between two anchors.
type Between func(newerHash, olderHash string) (int, error)

const maxOwnShown = 3

var kindRank = map[string]int{"head": 0, "commit": 1, "more-own": 2, "merge": 3, "more": 4, "fork": 5}

type item struct {
	row      Row
	branches []string // fork rows: branches joining here
}

type anchor struct {
	hash string
	time int64
}

// Layout builds the rows and assigns lanes to branches.
func Layout(t TrunkInput, branches []BranchInput, between Between) (Section, error) {
	items := []item{{row: Row{Kind: "head", Hash: t.HeadHash, Time: t.HeadTime}}}
	seen := map[string]bool{}
	anchors := []anchor{{t.HeadHash, t.HeadTime}}
	for _, b := range branches {
		if b.Merged {
			items = append(items, item{row: Row{Kind: "merge", Branch: b.Name, Time: b.LastCommit, Count: len(b.Own)}})
			continue
		}
		items = append(items, ownRows(b)...)
		if !seen[b.ForkHash] {
			seen[b.ForkHash] = true
			items = append(items, item{row: Row{Kind: "fork", Hash: b.ForkHash, Time: b.ForkTime}})
			anchors = append(anchors, anchor{b.ForkHash, b.ForkTime})
		}
	}
	forkIdx := map[string]int{}
	for i := range items {
		if items[i].row.Kind == "fork" {
			forkIdx[items[i].row.Hash] = i
		}
	}
	for _, b := range branches {
		if !b.Merged {
			i := forkIdx[b.ForkHash]
			items[i].branches = append(items[i].branches, b.Name)
		}
	}
	sort.SliceStable(anchors[1:], func(i, j int) bool {
		a, c := anchors[1+i], anchors[1+j]
		if a.time != c.time {
			return a.time > c.time
		}
		return a.hash < c.hash
	})
	for i := 1; i < len(anchors); i++ {
		n, err := between(anchors[i-1].hash, anchors[i].hash)
		if err != nil {
			return Section{}, err
		}
		if n > 0 {
			items = append(items, item{row: Row{Kind: "more", Time: anchors[i].time, Count: n}})
		}
	}
	sortItems(items)
	rows, lanes := assignLanes(items)
	return Section{Trunk: t.Name, Rows: rows, Lanes: lanes}, nil
}

func ownRows(b BranchInput) []item {
	var out []item
	if len(b.Own) <= maxOwnShown {
		for _, c := range b.Own {
			out = append(out, item{row: Row{Kind: "commit", Branch: b.Name, Hash: c.Hash, Time: c.Time}})
		}
		return out
	}
	out = append(out, item{row: Row{Kind: "commit", Branch: b.Name, Hash: b.Own[0].Hash, Time: b.Own[0].Time}})
	out = append(out, item{row: Row{Kind: "more-own", Branch: b.Name, Time: b.Own[1].Time, Count: len(b.Own) - 1}})
	return out
}

func sortItems(items []item) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i].row, items[j].row
		if a.Time != b.Time {
			return a.Time > b.Time
		}
		if kindRank[a.Kind] != kindRank[b.Kind] {
			return kindRank[a.Kind] < kindRank[b.Kind]
		}
		if a.Branch != b.Branch {
			return a.Branch < b.Branch
		}
		return a.Hash < b.Hash
	})
}

func assignLanes(items []item) ([]Row, int) {
	lane := map[string]int{}
	used := map[int]bool{}
	claim := func(name string) int {
		if l, ok := lane[name]; ok {
			return l
		}
		l := 1
		for used[l] {
			l++
		}
		used[l] = true
		lane[name] = l
		return l
	}
	rows := make([]Row, 0, len(items))
	maxLane := 0
	for _, it := range items {
		r := it.row
		r.Ends = []int{}
		switch r.Kind {
		case "commit", "more-own", "merge":
			r.Lane = claim(r.Branch)
			if r.Kind == "merge" {
				delete(used, r.Lane)
			}
		case "fork":
			for _, n := range it.branches {
				r.Ends = append(r.Ends, claim(n))
			}
			sort.Ints(r.Ends)
			for _, l := range r.Ends {
				delete(used, l)
			}
		}
		if r.Lane > maxLane {
			maxLane = r.Lane
		}
		for _, l := range r.Ends {
			if l > maxLane {
				maxLane = l
			}
		}
		rows = append(rows, r)
	}
	return rows, maxLane + 1
}
