package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestSwitchRecordsDefaults(t *testing.T) {
	s := newStore(t)
	rec, err := s.SwitchRecords("/some/repo")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Shelves == nil || len(rec.Shelves) != 0 || rec.Previous == nil || len(rec.Previous) != 0 {
		t.Fatalf("records = %#v, want empty non-nil lists", rec)
	}
}

func TestSwitchRecordsRoundTrip(t *testing.T) {
	s := newStore(t)
	in := SwitchRecords{
		Shelves: []Shelf{{
			Folder: "/some/repo", Branch: "feature/login", Hash: "0123456789abcdef0123456789abcdef01234567",
			Base: "89abcdef0123456789abcdef0123456789abcdef", Time: 1700000000, Files: []string{"a.txt", "new.txt"},
			State: ShelfKept, WhyCode: "base-moved", Why: "feature/login moved",
		}},
		Previous: []PreviousBranch{{Folder: "/some/repo", Branch: "feature/login"}},
	}
	if err := s.SaveSwitchRecords("/some/repo", in); err != nil {
		t.Fatal(err)
	}
	out, err := s.SwitchRecords("/some/repo")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("got %#v, want %#v", out, in)
	}
	// Each repository has its own file in the config dir, never in the repository.
	other, err := s.SwitchRecords("/other/repo")
	if err != nil || len(other.Shelves) != 0 {
		t.Fatalf("other repo = %#v, %v", other, err)
	}
	path := filepath.Join(s.Dir, "shelves", RepoKey("/some/repo")+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil || raw["path"] != "/some/repo" {
		t.Fatalf("file = %s, %v", data, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Dir(path))
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("shelves dir mode = %v, %v; want 0700", info.Mode().Perm(), err)
		}
	}
}

func TestSwitchRecordsNormalizesNilLists(t *testing.T) {
	s := newStore(t)
	if err := s.SaveSwitchRecords("/r", SwitchRecords{Shelves: []Shelf{{Hash: "h"}}}); err != nil {
		t.Fatal(err)
	}
	out, err := s.SwitchRecords("/r")
	if err != nil {
		t.Fatal(err)
	}
	if out.Previous == nil || out.Shelves[0].Files == nil {
		t.Fatalf("records = %#v, want non-nil lists", out)
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"shelves":[{"folder":"","branch":"","hash":"h","base":"","time":0,"files":[],"state":"pending","whyCode":"","why":""}],"previous":[]}`
	if string(data) != want {
		t.Fatalf("json = %s\nwant   %s", data, want)
	}
}

// A record without a state (written before states existed) is pending when
// it has no reason, and kept when it has one.
func TestSwitchRecordsStateOfOldRecords(t *testing.T) {
	s := newStore(t)
	in := SwitchRecords{Shelves: []Shelf{{Hash: "a"}, {Hash: "b", Why: "kept for a reason"}, {Hash: "c", State: ShelfSuperseded}}}
	if err := s.SaveSwitchRecords("/r", in); err != nil {
		t.Fatal(err)
	}
	out, err := s.SwitchRecords("/r")
	if err != nil {
		t.Fatal(err)
	}
	got := []string{out.Shelves[0].State, out.Shelves[1].State, out.Shelves[2].State}
	if !reflect.DeepEqual(got, []string{ShelfPending, ShelfKept, ShelfSuperseded}) {
		t.Fatalf("states = %v", got)
	}
}

func TestSwitchRecordsCorruptFileIsAnError(t *testing.T) {
	s := newStore(t)
	path := filepath.Join(s.Dir, "shelves", RepoKey("/r")+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A record that cannot be read must not be taken for "no shelves".
	if _, err := s.SwitchRecords("/r"); err == nil {
		t.Fatal("a corrupt records file must be an error")
	}
}
