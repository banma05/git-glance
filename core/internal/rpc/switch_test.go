package rpc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/banma05/git-glance/core/internal/testrepo"
)

// The switch result carries the shelf fields, and switchState says where the
// folder came from and which shelves are waiting.
func TestSwitchShelvesOverRPC(t *testing.T) {
	r := testrepo.New(t)
	r.Git(r.Dir, "switch", "-q", "-c", "feature/login")
	r.Commit(r.Dir, "login", map[string]string{"login.txt": "login\n"})
	if err := os.WriteFile(filepath.Join(r.Dir, "README"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := startServer(t, r)

	var raw map[string]json.RawMessage
	c.ok("switch", map[string]any{"repo": r.Dir, "folder": r.Dir, "branch": "main", "latest": true, "shelve": true}, &raw)
	keys := []string{}
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := []string{"aheadNote", "detail", "fastForwarded", "files", "keptShelf", "logError", "noFastForward", "ok", "path", "reason", "restored", "shelved"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("switch result keys = %v, want %v", keys, want)
	}
	var shelf map[string]json.RawMessage
	if err := json.Unmarshal(raw["shelved"], &shelf); err != nil {
		t.Fatal(err)
	}
	keys = keys[:0]
	for k := range shelf {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if want := []string{"base", "branch", "files", "folder", "hash", "state", "time", "why", "whyCode"}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("shelf keys = %v, want %v", keys, want)
	}
	if string(shelf["state"]) != `"pending"` || string(raw["noFastForward"]) != `""` {
		t.Fatalf("shelf state %s, noFastForward %s", shelf["state"], raw["noFastForward"])
	}
	var res struct {
		OK      bool `json:"ok"`
		Shelved *struct {
			Folder string   `json:"folder"`
			Branch string   `json:"branch"`
			Hash   string   `json:"hash"`
			Base   string   `json:"base"`
			Time   int64    `json:"time"`
			Files  []string `json:"files"`
			Why    string   `json:"why"`
		} `json:"shelved"`
		Restored  json.RawMessage `json:"restored"`
		KeptShelf json.RawMessage `json:"keptShelf"`
	}
	c.ok("switch", map[string]any{"repo": r.Dir, "folder": r.Dir, "branch": "feature/login", "shelve": true}, nil)
	c.ok("switch", map[string]any{"repo": r.Dir, "folder": r.Dir, "branch": "main", "shelve": true}, &res)
	if !res.OK || res.Shelved == nil || res.Shelved.Branch != "feature/login" || !isFullHash(res.Shelved.Hash) ||
		!isFullHash(res.Shelved.Base) || len(res.Shelved.Files) != 1 || string(res.Restored) != "null" || string(res.KeptShelf) != "null" {
		t.Fatalf("res = %+v", res)
	}

	var st struct {
		Previous string `json:"previous"`
		Shelves  []struct {
			Hash string `json:"hash"`
		} `json:"shelves"`
	}
	c.ok("switchState", map[string]any{"repo": r.Dir, "folder": r.Dir}, &st)
	if st.Previous != "feature/login" || len(st.Shelves) != 1 || st.Shelves[0].Hash != res.Shelved.Hash {
		t.Fatalf("state = %+v", st)
	}
	var back struct {
		OK       bool `json:"ok"`
		Restored *struct {
			Hash string `json:"hash"`
		} `json:"restored"`
	}
	c.ok("switch", map[string]any{"repo": r.Dir, "folder": r.Dir, "branch": "feature/login", "shelve": true}, &back)
	if !back.OK || back.Restored == nil || back.Restored.Hash != res.Shelved.Hash {
		t.Fatalf("back = %+v", back)
	}
	c.ok("switchState", map[string]any{"repo": r.Dir, "folder": r.Dir}, &st)
	if st.Previous != "main" || st.Shelves == nil || len(st.Shelves) != 0 {
		t.Fatalf("state = %+v", st)
	}
}

// Bad switch and switchState params are refused before any git runs.
func TestSwitchBadParamsMakeNoGitCall(t *testing.T) {
	r := testrepo.New(t)
	rec := newRecRunner()
	c := startServerWith(t, r, rec)
	rec.reset()
	for _, tc := range []struct {
		method string
		params map[string]any
	}{
		{"switch", map[string]any{"repo": r.Dir, "folder": r.Dir, "branch": "main", "remote": "-x"}},
		{"switch", map[string]any{"repo": r.Dir, "folder": r.Dir, "branch": "main", "remote": "or\nigin"}},
		{"switch", map[string]any{"repo": r.Dir, "folder": r.Dir, "branch": "main", "latest": "yes"}},
		{"switch", map[string]any{"repo": r.Dir, "folder": r.Dir, "branch": "-x"}},
		{"switchState", map[string]any{"repo": r.Dir}},
		{"switchState", map[string]any{"repo": r.Dir, "folder": "a\x00b"}},
	} {
		if code := c.errCode(tc.method, tc.params); code != "bad-params" {
			t.Errorf("%s %v gave %s", tc.method, tc.params, code)
		}
	}
	if got := rec.snapshot(); len(got) != 0 {
		t.Fatalf("git ran for refused params: %v", got)
	}
}
