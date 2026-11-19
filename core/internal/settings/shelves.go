package settings

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Shelf is uncommitted work of a folder that the tool put into git's stash
// before switching the folder away from Branch. Hash is the stash commit (it
// identifies the entry, whose stash@{n} index shifts), Base the commit HEAD
// was on, Time the unix time it was shelved and Files its first 50 paths.
// Why is "" or the reason the shelf was left in the stash; a stored shelf
// with a reason is never put back on its own.
type Shelf struct {
	Folder string   `json:"folder"`
	Branch string   `json:"branch"`
	Hash   string   `json:"hash"`
	Base   string   `json:"base"`
	Time   int64    `json:"time"`
	Files  []string `json:"files"`
	Why    string   `json:"why"`
}

// PreviousBranch is the branch a folder was on before the tool last switched
// it to another one.
type PreviousBranch struct {
	Folder string `json:"folder"`
	Branch string `json:"branch"`
}

// SwitchRecords is what the tool remembers about its switches in one
// repository. Shelves are in the order they were taken.
type SwitchRecords struct {
	Shelves  []Shelf          `json:"shelves"`
	Previous []PreviousBranch `json:"previous"`
}

type switchFile struct {
	Path    string        `json:"path"`
	Records SwitchRecords `json:"records"`
}

func (s Store) switchPath(repoRoot string) string {
	return filepath.Join(s.Dir, "shelves", RepoKey(repoRoot)+".json")
}

func normalizeSwitch(rec SwitchRecords) SwitchRecords {
	if rec.Shelves == nil {
		rec.Shelves = []Shelf{}
	}
	for i := range rec.Shelves {
		if rec.Shelves[i].Files == nil {
			rec.Shelves[i].Files = []string{}
		}
	}
	if rec.Previous == nil {
		rec.Previous = []PreviousBranch{}
	}
	return rec
}

// SwitchRecords returns the records of a repository, empty when there are
// none. A file that cannot be read is an error, never "no shelves".
func (s Store) SwitchRecords(repoRoot string) (SwitchRecords, error) {
	data, err := os.ReadFile(s.switchPath(repoRoot))
	if errors.Is(err, fs.ErrNotExist) {
		return normalizeSwitch(SwitchRecords{}), nil
	}
	if err != nil {
		return SwitchRecords{}, err
	}
	var f switchFile
	if err := json.Unmarshal(data, &f); err != nil {
		return SwitchRecords{}, err
	}
	return normalizeSwitch(f.Records), nil
}

// SaveSwitchRecords replaces the records of a repository (atomic write).
func (s Store) SaveSwitchRecords(repoRoot string, rec SwitchRecords) error {
	return writeJSON(s.switchPath(repoRoot), switchFile{Path: repoRoot, Records: normalizeSwitch(rec)})
}
