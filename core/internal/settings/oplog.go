package settings

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

type OpEntry struct {
	Time    int64    `json:"time"`
	Repo    string   `json:"repo"`
	Op      string   `json:"op"`
	Branch  string   `json:"branch"`
	Folder  string   `json:"folder"`
	Command []string `json:"command"`
	Before  string   `json:"before"`
	After   string   `json:"after"`
	Result  string   `json:"result"`
	Detail  string   `json:"detail"`
}

var opMu sync.Mutex

func (s Store) opPath() string { return filepath.Join(s.Dir, "oplog.jsonl") }

// AppendOp appends one JSON line to oplog.jsonl.
func (s Store) AppendOp(e OpEntry) error {
	if e.Command == nil {
		e.Command = []string{}
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	opMu.Lock()
	defer opMu.Unlock()
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.opPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(line); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Ops returns up to limit entries, newest first. An empty repoRoot means all
// repositories.
func (s Store) Ops(repoRoot string, limit int) ([]OpEntry, error) {
	opMu.Lock()
	defer opMu.Unlock()
	out := []OpEntry{}
	f, err := os.Open(s.opPath())
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var all []OpEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var e OpEntry
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		if repoRoot != "" && e.Repo != repoRoot {
			continue
		}
		all = append(all, e)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	for i := len(all) - 1; i >= 0 && (limit <= 0 || len(out) < limit); i-- {
		out = append(out, all[i])
	}
	return out, nil
}
