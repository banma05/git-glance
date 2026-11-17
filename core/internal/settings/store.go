// Package settings keeps the tool's own configuration, operation log and
// backup paths. Everything lives in the tool's config directory, never inside
// a user's repository. This package does not run git.
package settings

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type TrunkConfig struct {
	Name     string   `json:"name"`
	Test     string   `json:"test"`
	Releases []string `json:"releases"`
}

type RepoSettings struct {
	Trunks      []TrunkConfig     `json:"trunks"`
	HidePrefix  string            `json:"hidePrefix"`
	OldDays     int               `json:"oldDays"`
	ManualTrunk map[string]string `json:"manualTrunk"`
	CreatedBase map[string]string `json:"createdBase"`
}

type Global struct {
	Repos  []string `json:"repos"`
	Editor string   `json:"editor"` // code|cursor|windsurf|idea|pycharm|webstorm|goland
	Theme  string   `json:"theme"`  // auto|dark|light
}

type Store struct{ Dir string }

type repoFile struct {
	Path     string       `json:"path"`
	Settings RepoSettings `json:"settings"`
}

// Home returns $GITGLANCE_HOME, else <user config dir>/git-glance.
func Home() (string, error) {
	if v := os.Getenv("GITGLANCE_HOME"); v != "" {
		return v, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "git-glance"), nil
}

// RepoKey is the first 16 hex chars of the sha1 of the cleaned path
// (lower-cased first on Windows).
func RepoKey(repoRoot string) string {
	p := filepath.Clean(repoRoot)
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	sum := sha1.Sum([]byte(p))
	return hex.EncodeToString(sum[:])[:16]
}

func defaultRepo() RepoSettings {
	return RepoSettings{
		Trunks:      []TrunkConfig{},
		OldDays:     30,
		ManualTrunk: map[string]string{},
		CreatedBase: map[string]string{},
	}
}

func normalizeRepo(rs RepoSettings) RepoSettings {
	if rs.Trunks == nil {
		rs.Trunks = []TrunkConfig{}
	}
	for i := range rs.Trunks {
		if rs.Trunks[i].Releases == nil {
			rs.Trunks[i].Releases = []string{}
		}
	}
	if rs.ManualTrunk == nil {
		rs.ManualTrunk = map[string]string{}
	}
	if rs.CreatedBase == nil {
		rs.CreatedBase = map[string]string{}
	}
	return rs
}

func (s Store) Repo(repoRoot string) (RepoSettings, error) {
	data, err := os.ReadFile(filepath.Join(s.Dir, "repos", RepoKey(repoRoot)+".json"))
	if errors.Is(err, fs.ErrNotExist) {
		return defaultRepo(), nil
	}
	if err != nil {
		return RepoSettings{}, err
	}
	var f repoFile
	if err := json.Unmarshal(data, &f); err != nil {
		return RepoSettings{}, err
	}
	return normalizeRepo(f.Settings), nil
}

func (s Store) SaveRepo(repoRoot string, rs RepoSettings) error {
	return writeJSON(filepath.Join(s.Dir, "repos", RepoKey(repoRoot)+".json"),
		repoFile{Path: repoRoot, Settings: normalizeRepo(rs)})
}

func (s Store) Global() (Global, error) {
	g := Global{Repos: []string{}, Editor: "code", Theme: "auto"}
	data, err := os.ReadFile(filepath.Join(s.Dir, "global.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return g, nil
	}
	if err != nil {
		return Global{}, err
	}
	if err := json.Unmarshal(data, &g); err != nil {
		return Global{}, err
	}
	if g.Repos == nil {
		g.Repos = []string{}
	}
	if g.Editor == "" {
		g.Editor = "code"
	}
	if g.Theme == "" {
		g.Theme = "auto"
	}
	return g, nil
}

func (s Store) SaveGlobal(g Global) error {
	if g.Repos == nil {
		g.Repos = []string{}
	}
	return writeJSON(filepath.Join(s.Dir, "global.json"), g)
}

// BackupDir returns <Dir>/backups/<key>/<folderName>-<yyyymmdd-hhmmss>.
func (s Store) BackupDir(repoRoot, folderName string, t time.Time) string {
	return filepath.Join(s.Dir, "backups", RepoKey(repoRoot), folderName+"-"+t.Format("20060102-150405"))
}

func (s Store) BackupsSize() (int64, error) {
	var total int64
	root := filepath.Join(s.Dir, "backups")
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	return total, err
}

func (s Store) ClearBackups() error {
	return os.RemoveAll(filepath.Join(s.Dir, "backups"))
}

// writeJSON writes a temp file in the target directory, then renames it over
// the target.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
