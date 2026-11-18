package rpc

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/settings"
	"github.com/banma05/git-glance/core/internal/testrepo"
)

// recRunner records every git call and can hold some of them back.
type recRunner struct {
	gitx.Runner
	mu     sync.Mutex
	calls  [][]string
	capped []int64
	before func(args []string)
}

func newRecRunner() *recRunner { return &recRunner{Runner: gitx.ExecRunner{}} }

func (r *recRunner) note(args []string, limit int64) {
	r.mu.Lock()
	r.calls = append(r.calls, append([]string(nil), args...))
	if limit > 0 {
		r.capped = append(r.capped, limit)
	}
	r.mu.Unlock()
	if r.before != nil {
		r.before(args)
	}
}

func (r *recRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	r.note(args, 0)
	return r.Runner.Run(ctx, dir, args...)
}

func (r *recRunner) RunCapped(ctx context.Context, dir string, limit int64, args ...string) ([]byte, bool, error) {
	r.note(args, limit)
	return r.Runner.RunCapped(ctx, dir, limit, args...)
}

func (r *recRunner) reset() {
	r.mu.Lock()
	r.calls, r.capped = nil, nil
	r.mu.Unlock()
}

func (r *recRunner) snapshot() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]string(nil), r.calls...)
}

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func TestBadRevisionMakesNoGitCall(t *testing.T) {
	r := forkedRepo(t)
	rec := newRecRunner()
	c := startServerWith(t, r, rec)
	rec.reset()
	for _, bad := range []string{"", "-x", "--output=" + filepath.Join(r.Root, "pwned.txt"), "--git-dir=" + r.Dir} {
		calls := []struct {
			method string
			params map[string]any
		}{
			{"compare", map[string]any{"repo": r.Dir, "a": bad, "b": "b"}},
			{"compare", map[string]any{"repo": r.Dir, "a": "a", "b": bad}},
			{"fileAt", map[string]any{"repo": r.Dir, "ref": bad, "path": "a.txt"}},
			{"fileDiff", map[string]any{"repo": r.Dir, "a": bad, "b": "b", "path": "a.txt"}},
			{"fileDiff", map[string]any{"repo": r.Dir, "a": "a", "b": bad, "path": "a.txt"}},
			{"trunkCommits", map[string]any{"repo": r.Dir, "newer": bad, "older": "a", "limit": 5}},
			{"trunkCommits", map[string]any{"repo": r.Dir, "newer": "a", "older": bad, "limit": 5}},
		}
		for _, tc := range calls {
			if code := c.errCode(tc.method, tc.params); code != "bad-params" {
				t.Errorf("%s %q gave %s", tc.method, bad, code)
			}
		}
	}
	if got := rec.snapshot(); len(got) != 0 {
		t.Fatalf("git ran for refused revisions: %v", got)
	}
}

func TestReadCommandsCarrySafetyFlags(t *testing.T) {
	r := forkedRepo(t)
	rec := newRecRunner()
	c := startServerWith(t, r, rec)
	rec.reset()
	c.ok("compare", map[string]any{"repo": r.Dir, "a": "a", "b": "b"}, nil)
	c.ok("fileAt", map[string]any{"repo": r.Dir, "ref": "b", "path": "b1.txt"}, nil)
	c.ok("fileDiff", map[string]any{"repo": r.Dir, "a": "a", "b": "b", "path": "b1.txt"}, nil)
	c.ok("trunkCommits", map[string]any{"repo": r.Dir, "newer": "b", "older": "main", "limit": 5}, nil)
	seen := map[string]int{}
	for _, args := range rec.snapshot() {
		sub := args[0]
		switch sub {
		case "log", "show", "diff":
			seen[sub]++
		}
		if (sub == "log" || sub == "show") && !hasArg(args, "--no-show-signature") {
			t.Errorf("%v lacks --no-show-signature", args)
		}
		if sub == "show" || sub == "diff" {
			for _, f := range []string{"--no-ext-diff", "--no-textconv", "--no-color"} {
				if !hasArg(args, f) {
					t.Errorf("%v lacks %s", args, f)
				}
			}
		}
	}
	for _, sub := range []string{"log", "show", "diff"} {
		if seen[sub] == 0 {
			t.Errorf("no %s call was made, the check is empty", sub)
		}
	}
}

func TestNoExternalDiffOrTextconv(t *testing.T) {
	r := testrepo.New(t)
	marker := r.Root + "/marker-ext"
	markerConv := r.Root + "/marker-conv"
	r.Commit(r.Dir, "attrs", map[string]string{".gitattributes": "*.dat diff=drv\n", "f.dat": "one\n", "f.txt": "one\n"})
	r.Branch("x", "main")
	r.Git(r.Dir, "checkout", "x")
	r.Commit(r.Dir, "change", map[string]string{"f.dat": "two\n", "f.txt": "two\n"})
	r.Git(r.Dir, "checkout", "main")
	r.Git(r.Dir, "config", "diff.external", "echo x > '"+marker+"' #")
	r.Git(r.Dir, "config", "diff.drv.textconv", "echo x > '"+markerConv+"' #")
	// The settings must really fire for an unprotected diff, or the test proves nothing.
	r.Git(r.Dir, "diff", "main", "x", "--", "f.txt")
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("diff.external fixture does not fire: %v", err)
	}
	r.Git(r.Dir, "config", "--unset", "diff.external")
	r.Git(r.Dir, "diff", "main", "x", "--", "f.dat")
	if _, err := os.Stat(markerConv); err != nil {
		t.Fatalf("textconv fixture does not fire: %v", err)
	}
	os.Remove(marker)
	os.Remove(markerConv)

	c := startServer(t, r)
	// Phase 1: an external diff program is configured.
	r.Git(r.Dir, "config", "diff.external", "echo x > '"+marker+"' #")
	c.ok("compare", map[string]any{"repo": r.Dir, "a": "main", "b": "x"}, nil)
	c.ok("fileAt", map[string]any{"repo": r.Dir, "ref": "x", "path": "f.dat"}, nil)
	c.ok("fileDiff", map[string]any{"repo": r.Dir, "a": "main", "b": "x", "path": "f.txt"}, nil)
	c.ok("fileDiff", map[string]any{"repo": r.Dir, "a": "main", "b": "x", "path": "f.dat"}, nil)
	// Phase 2: only the textconv filter is configured.
	r.Git(r.Dir, "config", "--unset", "diff.external")
	c.ok("fileDiff", map[string]any{"repo": r.Dir, "a": "main", "b": "x", "path": "f.dat"}, nil)
	for _, m := range []string{marker, markerConv} {
		if _, err := os.Stat(m); err == nil {
			t.Fatalf("a configured program ran: %s", m)
		}
	}
}

func TestFileReadsAreCapped(t *testing.T) {
	r := testrepo.New(t)
	r.Commit(r.Dir, "big", map[string]string{"big.txt": strings.Repeat("y", maxText+5000)})
	r.Branch("x", "main")
	r.Git(r.Dir, "checkout", "x")
	r.Commit(r.Dir, "bigger", map[string]string{"big.txt": strings.Repeat("z", maxText+5000)})
	r.Git(r.Dir, "checkout", "main")
	rec := newRecRunner()
	c := startServerWith(t, r, rec)
	rec.reset()
	var ft struct {
		Text      string `json:"text"`
		Truncated bool   `json:"truncated"`
	}
	c.ok("fileAt", map[string]any{"repo": r.Dir, "ref": "main", "path": "big.txt"}, &ft)
	if !ft.Truncated || len(ft.Text) != maxText {
		t.Fatalf("fileAt truncated=%v len=%d", ft.Truncated, len(ft.Text))
	}
	var fd struct {
		Diff      string `json:"diff"`
		Truncated bool   `json:"truncated"`
	}
	c.ok("fileDiff", map[string]any{"repo": r.Dir, "a": "main", "b": "x", "path": "big.txt"}, &fd)
	if !fd.Truncated || len(fd.Diff) != maxText {
		t.Fatalf("fileDiff truncated=%v len=%d", fd.Truncated, len(fd.Diff))
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.capped) != 2 || rec.capped[0] != maxText || rec.capped[1] != maxText {
		t.Fatalf("fileAt and fileDiff must read through RunCapped(%d): %v", maxText, rec.capped)
	}
}

func TestWriteSetIsPinned(t *testing.T) {
	h, err := NewHandler(gitx.ExecRunner{}, settings.Store{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, m := range []string{"align", "switch", "createFolder", "removeFolder", "setRole", "moveTrunk", "assign",
		"setRepoSettings", "setGlobal", "addRepo", "removeRepo", "fetch", "clearBackups"} {
		want[m] = true
	}
	for name, m := range h.methods {
		if m.write != want[name] {
			t.Errorf("method %s: write=%v, want %v", name, m.write, want[name])
		}
	}
	for name := range want {
		if _, ok := h.methods[name]; !ok {
			t.Errorf("write method %s is missing", name)
		}
	}
}

func (c *client) readReplyWithin(d time.Duration) (reply, bool) {
	c.t.Helper()
	ch := make(chan reply, 1)
	go func() {
		line, err := c.r.ReadBytes('\n')
		if err != nil {
			return
		}
		var rep reply
		json.Unmarshal(line, &rep)
		ch <- rep
	}()
	select {
	case rep := <-ch:
		return rep, true
	case <-time.After(d):
		return reply{}, false
	}
}

func TestQueuedWritesDoNotBlockReads(t *testing.T) {
	r := testrepo.New(t)
	rec := newRecRunner()
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	rec.before = func(args []string) {
		if args[0] == "fetch" {
			started <- struct{}{}
			<-release
		}
	}
	c := startServerWith(t, r, rec)
	c.sendRaw(`{"id":1,"method":"fetch","params":{"repo":"` + r.Dir + `"}}`)
	<-started
	const queued = 40
	send := func(line string) { io.WriteString(c.w, line+"\n") }
	go func() { // a server that stops reading would block these writes
		for i := 0; i < queued; i++ {
			theme := []string{"dark", "light"}[i%2]
			send(`{"id":` + strconv.Itoa(2+i) + `,"method":"setGlobal","params":{"theme":"` + theme + `"}}`)
		}
		send(`{"id":100,"method":"version"}`)
		send(`{"id":101,"method":"view","params":{"repo":"` + r.Dir + `"}}`)
	}()
	got := map[int64]bool{}
	for len(got) < 2 {
		rep, ok := c.readReplyWithin(10 * time.Second)
		if !ok {
			close(release)
			t.Fatalf("reads did not answer while writes were queued (answered: %v)", got)
		}
		if rep.ID < 100 {
			close(release)
			t.Fatalf("a queued write answered before the running one finished: id %d", rep.ID)
		}
		got[rep.ID] = true
	}
	close(release)
	for want := int64(1); want <= 1+queued; want++ {
		rep, ok := c.readReplyWithin(20 * time.Second)
		if !ok || rep.ID != want {
			t.Fatalf("write reply %d: got id %d (ok=%v), writes must finish in arrival order", want, rep.ID, ok)
		}
	}
}

// versionRunner answers `git version` with text and runs everything else.
type versionRunner struct {
	gitx.ExecRunner
	text string
}

func (v versionRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if len(args) == 1 && args[0] == "version" {
		return []byte(v.text), nil
	}
	return v.ExecRunner.Run(ctx, dir, args...)
}

// Older git lacks flags the writes depend on (--no-overwrite-ignore,
// --no-autostash): every write is refused, while version keeps reporting.
func TestWritesNeedGit230(t *testing.T) {
	for _, c := range []struct {
		name, git string
		runner    gitx.Runner
	}{
		{"old git", "2.29", versionRunner{text: "git version 2.29.2\n"}},
		{"no git", "", gitx.ExecRunner{Git: filepath.Join(t.TempDir(), "no-such-git")}},
	} {
		h, err := NewHandler(c.runner, settings.Store{Dir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		for name, m := range h.methods {
			if !m.write {
				continue
			}
			rep := h.Handle(context.Background(), Request{ID: 1, Method: name, Params: json.RawMessage(`{}`)})
			if rep.Error == nil || rep.Error.Code != "git-too-old" {
				t.Errorf("%s: %s gave %+v, want git-too-old", c.name, name, rep)
			}
		}
		rep := h.Handle(context.Background(), Request{ID: 2, Method: "version"})
		res, ok := rep.Result.(VersionResult)
		if rep.Error != nil || !ok || res.GitOK || res.Git != c.git {
			t.Errorf("%s: version = %+v", c.name, rep)
		}
	}
}

// The handler keeps a cache per repository: viewing an unchanged repository
// again runs no per-branch git command.
func TestRepeatedViewUsesTheCache(t *testing.T) {
	r := testrepo.New(t)
	for _, b := range []string{"feature/a", "feature/b", "feature/c"} {
		r.Branch(b, "main")
		r.Git(r.Dir, "checkout", "-q", b)
		r.Commit(r.Dir, b, map[string]string{strings.ReplaceAll(b, "/", "-") + ".txt": "x\n"})
		r.Git(r.Dir, "checkout", "-q", "main")
	}
	rec := newRecRunner()
	c := startServerWith(t, r, rec)
	c.ok("view", map[string]any{"repo": r.Dir}, nil)
	rec.reset()
	c.ok("view", map[string]any{"repo": r.Dir}, nil)
	for _, call := range rec.snapshot() {
		switch call[0] {
		case "merge-base", "cherry", "log", "diff", "patch-id", "rev-list":
			t.Fatalf("the second view ran %q", call)
		}
	}
}
