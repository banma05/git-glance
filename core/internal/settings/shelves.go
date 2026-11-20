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
// was on, Time the unix time it was shelved, Files its first 50 paths and
// FileCount the number of all its paths (at least len(Files)). State is one
// of the Shelf* states; only a pending shelf is ever put back on its own.
// WhyCode is "" or a machine-readable reason (the ops package lists them)
// and Why the same reason as an English sentence. Taken lists, with WhyCode
// path-exists, every path the shelf would write where something is already
// on disk; it is empty otherwise.
type Shelf struct {
	Folder    string   `json:"folder"`
	Branch    string   `json:"branch"`
	Hash      string   `json:"hash"`
	Base      string   `json:"base"`
	Time      int64    `json:"time"`
	Files     []string `json:"files"`
	FileCount int      `json:"fileCount"`
	State     string   `json:"state"`
	WhyCode   string   `json:"whyCode"`
	Why       string   `json:"why"`
	Taken     []string `json:"taken"`
}

// Shelf states. Pending: in the stash, put back on the folder's next arrival
// on its branch when that cannot conflict. Kept: in the stash, never put
// back on its own (git did not do what it should, or putting it back after a
// failed switch was not possible). Superseded: a newer shelf of the same
// folder and branch was taken. Restored: put back and its stash entry
// dropped. RestoredEntryKept: put back, but its stash entry stays. Orphan: a
// stash entry with a Git Glance shelf message and no record.
const (
	ShelfPending           = "pending"
	ShelfKept              = "kept"
	ShelfSuperseded        = "superseded"
	ShelfRestored          = "restored"
	ShelfRestoredEntryKept = "restored-entry-kept"
	ShelfOrphan            = "orphan"
)

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
		sh := &rec.Shelves[i]
		if sh.Files == nil {
			sh.Files = []string{}
		}
		if sh.FileCount < len(sh.Files) {
			sh.FileCount = len(sh.Files)
		}
		if sh.Taken == nil {
			sh.Taken = []string{}
		}
		switch {
		case sh.State != "":
		case sh.Why == "":
			sh.State = ShelfPending
		default:
			sh.State = ShelfKept
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
