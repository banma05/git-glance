package graph

import (
	"reflect"
	"testing"

	"github.com/banma05/git-glance/core/internal/repo"
)

func ci(h string, t int64) repo.CommitInfo { return repo.CommitInfo{Hash: h, Time: t} }

func TestLayout(t *testing.T) {
	tr := TrunkInput{Name: "main", HeadHash: "H", HeadTime: 100}
	bs := []BranchInput{
		{Name: "A", Own: []repo.CommitInfo{ci("a2", 99), ci("a1", 98)}, ForkHash: "F1", ForkTime: 90},
		{Name: "B", Own: []repo.CommitInfo{ci("b1", 97)}, ForkHash: "F1", ForkTime: 90},
		{Name: "C", Own: []repo.CommitInfo{ci("c5", 96), ci("c4", 95), ci("c3", 94), ci("c2", 93), ci("c1", 92)}, ForkHash: "F2", ForkTime: 80},
		{Name: "D", Own: []repo.CommitInfo{ci("d2", 86), ci("d1", 84)}, Merged: true, LastCommit: 85},
	}
	table := map[[2]string]int{{"H", "F1"}: 6, {"F1", "F2"}: 3}
	between := func(n, o string) (int, error) { return table[[2]string{n, o}], nil }
	s, err := Layout(tr, bs, between)
	if err != nil {
		t.Fatal(err)
	}
	type w struct {
		Kind   string
		Lane   int
		Branch string
		Count  int
		Ends   []int
	}
	want := []w{
		{"head", 0, "", 0, []int{}}, {"commit", 1, "A", 0, []int{}}, {"commit", 1, "A", 0, []int{}},
		{"commit", 2, "B", 0, []int{}}, {"commit", 3, "C", 0, []int{}}, {"more-own", 3, "C", 4, []int{}},
		{"more", 0, "", 6, []int{}}, {"fork", 0, "", 0, []int{1, 2}}, {"merge", 1, "D", 2, []int{}},
		{"more", 0, "", 3, []int{}}, {"fork", 0, "", 0, []int{3}},
	}
	var got []w
	for _, r := range s.Rows {
		got = append(got, w{r.Kind, r.Lane, r.Branch, r.Count, r.Ends})
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rows:\n got %v\nwant %v", got, want)
	}
	if s.Lanes != 4 || s.Trunk != "main" {
		t.Fatalf("lanes=%d trunk=%q", s.Lanes, s.Trunk)
	}
	if s.Rows[5].Time != 95 || s.Rows[8].Time != 85 || s.Rows[6].Time != 90 {
		t.Fatalf("times wrong: %+v", s.Rows)
	}
}

func TestLayoutEmpty(t *testing.T) {
	s, err := Layout(TrunkInput{Name: "main", HeadHash: "H", HeadTime: 5}, nil, func(string, string) (int, error) { return 0, nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Rows) != 1 || s.Rows[0].Kind != "head" || s.Lanes != 1 || s.Rows[0].Ends == nil {
		t.Fatalf("%+v", s)
	}
}
