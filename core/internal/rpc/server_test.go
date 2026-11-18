package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/repo"
	"github.com/banma05/git-glance/core/internal/settings"
	"github.com/banma05/git-glance/core/internal/testrepo"
)

type reply struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *RPCError       `json:"error"`
}

type client struct {
	t    *testing.T
	h    *Handler
	w    *io.PipeWriter
	r    *bufio.Reader
	done chan error
	next int64
}

// startServer runs ServeLines over pipes against a handler that stores its
// settings in a temp dir. The test repo's isolated git config is applied.
func startServer(t *testing.T, repo *testrepo.Repo) *client {
	t.Helper()
	return startServerWith(t, repo, gitx.ExecRunner{})
}

func startServerWith(t *testing.T, repo *testrepo.Repo, runner gitx.Runner) *client {
	t.Helper()
	if repo != nil {
		repo.ApplyEnv()
	}
	h, err := NewHandler(runner, settings.Store{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	c := &client{t: t, h: h, w: inW, r: bufio.NewReaderSize(outR, 1<<20), done: make(chan error, 1)}
	go func() {
		err := ServeLines(context.Background(), h, inR, outW)
		outW.Close()
		c.done <- err
	}()
	t.Cleanup(func() {
		inW.Close()
		select {
		case <-c.done:
		case <-time.After(10 * time.Second):
			t.Error("ServeLines did not return after its input was closed")
		}
	})
	return c
}

func (c *client) sendRaw(line string) {
	c.t.Helper()
	if _, err := io.WriteString(c.w, line+"\n"); err != nil {
		c.t.Fatal(err)
	}
}

func (c *client) readReply() reply {
	c.t.Helper()
	line, err := c.r.ReadBytes('\n')
	if err != nil {
		c.t.Fatalf("reading reply: %v", err)
	}
	var rep reply
	if err := json.Unmarshal(line, &rep); err != nil {
		c.t.Fatalf("bad reply %q: %v", line, err)
	}
	return rep
}

// call sends one request and returns its reply (requests are sequential).
func (c *client) call(method string, params any) reply {
	c.t.Helper()
	c.next++
	req := map[string]any{"id": c.next, "method": method}
	if params != nil {
		req["params"] = params
	}
	b, err := json.Marshal(req)
	if err != nil {
		c.t.Fatal(err)
	}
	c.sendRaw(string(b))
	rep := c.readReply()
	if rep.ID != c.next {
		c.t.Fatalf("reply id %d, want %d", rep.ID, c.next)
	}
	return rep
}

// ok calls and decodes the result into out (when non-nil).
func (c *client) ok(method string, params any, out any) {
	c.t.Helper()
	rep := c.call(method, params)
	if rep.Error != nil {
		c.t.Fatalf("%s: error %s: %s", method, rep.Error.Code, rep.Error.Message)
	}
	if out != nil {
		if err := json.Unmarshal(rep.Result, out); err != nil {
			c.t.Fatalf("%s: decoding %s: %v", method, rep.Result, err)
		}
	}
}

func (c *client) errCode(method string, params any) string {
	c.t.Helper()
	rep := c.call(method, params)
	if rep.Error == nil {
		c.t.Fatalf("%s: want an error, got result %s", method, rep.Result)
	}
	return rep.Error.Code
}

func TestVersion(t *testing.T) {
	c := startServer(t, nil)
	c.sendRaw(`{"id":1,"method":"version"}`)
	rep := c.readReply()
	if rep.ID != 1 || rep.Error != nil {
		t.Fatalf("reply %+v", rep)
	}
	var v struct {
		Core  string `json:"core"`
		Git   string `json:"git"`
		GitOK bool   `json:"gitOK"`
	}
	if err := json.Unmarshal(rep.Result, &v); err != nil {
		t.Fatal(err)
	}
	if !v.GitOK || v.Git == "" || v.Core == "" {
		t.Fatalf("version result %+v", v)
	}
}

func TestVersionWithoutGit(t *testing.T) {
	h, err := NewHandler(gitx.ExecRunner{Git: filepath.Join(t.TempDir(), "no-such-git")}, settings.Store{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	rep := h.Handle(context.Background(), Request{ID: 1, Method: "version"})
	if rep.Error != nil {
		t.Fatalf("missing git must not be an error: %+v", rep.Error)
	}
	if res, ok := rep.Result.(VersionResult); !ok || res.GitOK {
		t.Fatalf("result %+v", rep.Result)
	}
}

func TestUnknownMethod(t *testing.T) {
	c := startServer(t, nil)
	c.sendRaw(`{"id":2,"method":"push"}`)
	rep := c.readReply()
	if rep.ID != 2 || rep.Error == nil || rep.Error.Code != "unknown-method" {
		t.Fatalf("reply %+v", rep)
	}
}

func TestBadJSONLine(t *testing.T) {
	c := startServer(t, nil)
	c.sendRaw(`{not json`)
	rep := c.readReply()
	if rep.ID != 0 || rep.Error == nil || rep.Error.Code != "bad-params" {
		t.Fatalf("reply %+v", rep)
	}
	if code := c.errCode("push", nil); code != "unknown-method" {
		t.Fatalf("server stopped serving: %s", code)
	}
}

func TestOversizedLine(t *testing.T) {
	c := startServer(t, nil)
	go func() {
		io.WriteString(c.w, strings.Repeat("x", 2<<20)+"\n")
		io.WriteString(c.w, `{"id":7,"method":"version"}`+"\n")
	}()
	rep := c.readReply()
	if rep.ID != 0 || rep.Error == nil || rep.Error.Code != "bad-params" {
		t.Fatalf("oversized line reply %+v", rep)
	}
	rep = c.readReply()
	if rep.ID != 7 || rep.Error != nil {
		t.Fatalf("server did not keep serving: %+v", rep)
	}
}

func TestServeLinesWaitsForInFlight(t *testing.T) {
	r := testrepo.New(t)
	r.ApplyEnv()
	slow := &slowRunner{Runner: gitx.ExecRunner{}, release: make(chan struct{}), started: make(chan struct{}, 1)}
	h, err := NewHandler(slow, settings.Store{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	inR, inW := io.Pipe()
	var out lockedBuffer
	done := make(chan error, 1)
	go func() { done <- ServeLines(context.Background(), h, inR, &out) }()
	io.WriteString(inW, `{"id":1,"method":"discover","params":{"path":"`+r.Dir+`"}}`+"\n")
	<-slow.started
	inW.Close() // EOF while the request is still running
	select {
	case <-done:
		t.Fatal("ServeLines returned before the in-flight request finished")
	case <-time.After(200 * time.Millisecond):
	}
	close(slow.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"id":1`) {
		t.Fatalf("no reply written: %q", out.String())
	}
}

type slowRunner struct {
	gitx.Runner
	release chan struct{}
	started chan struct{}
}

func (s *slowRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if len(args) > 0 && args[0] == "worktree" {
		select {
		case s.started <- struct{}{}:
		default:
		}
		<-s.release
	}
	return s.Runner.Run(ctx, dir, args...)
}

type lockedBuffer struct {
	mu sync.Mutex
	sb strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sb.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sb.String()
}

func TestViewOverRPC(t *testing.T) {
	r := testrepo.New(t)
	r.Branch("feature/login", "main")
	r.Git(r.Dir, "checkout", "feature/login")
	r.Commit(r.Dir, "login", map[string]string{"login.txt": "x\n"})
	r.Git(r.Dir, "checkout", "main")
	c := startServer(t, r)

	rep := c.call("view", map[string]any{"repo": r.Dir})
	if rep.Error != nil {
		t.Fatalf("view: %+v", rep.Error)
	}
	var v struct {
		Groups []struct {
			Trunk struct {
				Name string `json:"name"`
			} `json:"trunk"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(rep.Result, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Groups) == 0 || v.Groups[0].Trunk.Name != "main" {
		t.Fatalf("groups %s", rep.Result)
	}
	if !strings.Contains(string(rep.Result), `"behindTrunk"`) {
		t.Fatalf("camelCase key behindTrunk missing: %s", rep.Result)
	}
	for _, key := range []string{`"unknown":[]`, `"releases":[]`} {
		if !strings.Contains(string(rep.Result), key) {
			t.Fatalf("%s missing in %s", key, rep.Result)
		}
	}
}

func TestViewCarriesLastFetch(t *testing.T) {
	r := testrepo.New(t)
	c := startServer(t, r)
	var fr struct {
		OK bool  `json:"ok"`
		At int64 `json:"at"`
	}
	c.ok("fetch", map[string]any{"repo": r.Dir}, &fr)
	if !fr.OK {
		t.Fatalf("fetch %+v", fr)
	}
	var v struct {
		FetchedAt  int64  `json:"fetchedAt"`
		FetchError string `json:"fetchError"`
	}
	c.ok("view", map[string]any{"repo": r.Dir}, &v)
	if v.FetchedAt != fr.At || v.FetchError != "" {
		t.Fatalf("view fetchedAt %d error %q, fetch at %d", v.FetchedAt, v.FetchError, fr.At)
	}
}

func TestSetRole(t *testing.T) {
	r := testrepo.New(t)
	r.Branch("main-test", "main")
	c := startServer(t, r)

	var rs settings.RepoSettings
	c.ok("setRole", map[string]any{"repo": r.Dir, "branch": "main-test", "role": "test", "trunk": "main"}, &rs)
	if len(rs.Trunks) == 0 || rs.Trunks[0].Name != "main" || rs.Trunks[0].Test != "main-test" {
		t.Fatalf("after test role: %+v", rs.Trunks)
	}
	c.ok("setRole", map[string]any{"repo": r.Dir, "branch": "main-test", "role": "none"}, &rs)
	if len(rs.Trunks) == 0 || rs.Trunks[0].Test != "" {
		t.Fatalf("after none role: %+v", rs.Trunks)
	}
	if code := c.errCode("setRole", map[string]any{"repo": r.Dir, "branch": "x", "role": "bogus"}); code != "bad-params" {
		t.Fatalf("bogus role gave %s", code)
	}
	if code := c.errCode("setRole", map[string]any{"repo": r.Dir, "branch": "x", "role": "test", "trunk": "nope"}); code != "bad-params" {
		t.Fatalf("unknown trunk gave %s", code)
	}
}

func TestSetRoleTrunkKeepsImplicitDefault(t *testing.T) {
	r := testrepo.New(t)
	r.Branch("v2", "main")
	c := startServer(t, r)
	var rs settings.RepoSettings
	c.ok("setRole", map[string]any{"repo": r.Dir, "branch": "v2", "role": "trunk"}, &rs)
	if len(rs.Trunks) != 2 || rs.Trunks[0].Name != "main" || rs.Trunks[1].Name != "v2" {
		t.Fatalf("trunks %+v", rs.Trunks)
	}
	c.ok("moveTrunk", map[string]any{"repo": r.Dir, "name": "v2", "index": 0}, &rs)
	if rs.Trunks[0].Name != "v2" {
		t.Fatalf("trunks after move %+v", rs.Trunks)
	}
	c.ok("assign", map[string]any{"repo": r.Dir, "branch": "feature/x", "trunk": "main"}, &rs)
	if rs.ManualTrunk["feature/x"] != "main" {
		t.Fatalf("manual %+v", rs.ManualTrunk)
	}
	c.ok("setRepoSettings", map[string]any{"repo": r.Dir, "hidePrefix": "alice/", "oldDays": 14}, &rs)
	if rs.HidePrefix != "alice/" || rs.OldDays != 14 {
		t.Fatalf("settings %+v", rs)
	}
}

type commitList []struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
}

// forkedRepo makes branches a (1 own commit) and b (2 own commits) off main.
func forkedRepo(t *testing.T) *testrepo.Repo {
	r := testrepo.New(t)
	r.Branch("a", "main")
	r.Branch("b", "main")
	r.Git(r.Dir, "checkout", "a")
	r.Commit(r.Dir, "a1", map[string]string{"a.txt": "a\n"})
	r.Git(r.Dir, "checkout", "b")
	r.Commit(r.Dir, "b1", map[string]string{"b1.txt": "b1\n"})
	r.Commit(r.Dir, "b2", map[string]string{"b2.txt": "b2\n"})
	r.Git(r.Dir, "checkout", "main")
	return r
}

func TestCompare(t *testing.T) {
	r := forkedRepo(t)
	c := startServer(t, r)
	var res struct {
		OnlyA commitList `json:"onlyA"`
		OnlyB commitList `json:"onlyB"`
		Files []struct {
			Path   string `json:"path"`
			Status string `json:"status"`
		} `json:"files"`
	}
	c.ok("compare", map[string]any{"repo": r.Dir, "a": "a", "b": "b"}, &res)
	if len(res.OnlyA) != 1 || len(res.OnlyB) != 2 {
		t.Fatalf("onlyA %d onlyB %d", len(res.OnlyA), len(res.OnlyB))
	}
	got := map[string]string{}
	for _, f := range res.Files {
		got[f.Path] = f.Status
	}
	want := map[string]string{"a.txt": "D", "b1.txt": "A", "b2.txt": "A"}
	if len(got) != len(want) {
		t.Fatalf("files %+v", res.Files)
	}
	for p, s := range want {
		if got[p] != s {
			t.Fatalf("files %+v, want %v", res.Files, want)
		}
	}
}

func TestCompareEmptyListsAreArrays(t *testing.T) {
	r := testrepo.New(t)
	c := startServer(t, r)
	rep := c.call("compare", map[string]any{"repo": r.Dir, "a": "main", "b": "main"})
	if rep.Error != nil {
		t.Fatal(rep.Error)
	}
	for _, key := range []string{`"onlyA":[]`, `"onlyB":[]`, `"files":[]`} {
		if !strings.Contains(string(rep.Result), key) {
			t.Fatalf("%s missing in %s", key, rep.Result)
		}
	}
}

func TestRevisionStartingWithDashIsRefused(t *testing.T) {
	r := forkedRepo(t)
	c := startServer(t, r)
	evil := "--output=" + filepath.Join(r.Root, "pwned.txt")
	calls := []struct {
		method string
		params map[string]any
	}{
		{"compare", map[string]any{"repo": r.Dir, "a": evil, "b": "b"}},
		{"compare", map[string]any{"repo": r.Dir, "a": "a", "b": "-x"}},
		{"fileAt", map[string]any{"repo": r.Dir, "ref": evil, "path": "a.txt"}},
		{"fileDiff", map[string]any{"repo": r.Dir, "a": "a", "b": evil, "path": "a.txt"}},
		{"trunkCommits", map[string]any{"repo": r.Dir, "newer": evil, "older": "a", "limit": 5}},
		{"trunkCommits", map[string]any{"repo": r.Dir, "newer": "a", "older": "", "limit": 5}},
	}
	for _, tc := range calls {
		if code := c.errCode(tc.method, tc.params); code != "bad-params" {
			t.Errorf("%s %v gave %s, want bad-params", tc.method, tc.params, code)
		}
	}
	if _, err := os.Stat(filepath.Join(r.Root, "pwned.txt")); err == nil {
		t.Fatal("a caller-supplied option was executed")
	}
	if code := c.errCode("compare", map[string]any{"repo": r.Dir, "a": "no-such-branch", "b": "b"}); code != "bad-params" {
		t.Errorf("unknown revision gave %s", code)
	}
}

func TestFileAt(t *testing.T) {
	r := testrepo.New(t)
	r.Commit(r.Dir, "files", map[string]string{
		"dir/t.txt": "hello\n",
		"bin.dat":   "a\x00b",
		"big.txt":   strings.Repeat("y", maxText+100),
	})
	c := startServer(t, r)
	var res struct {
		Text      string `json:"text"`
		Binary    bool   `json:"binary"`
		Truncated bool   `json:"truncated"`
	}
	c.ok("fileAt", map[string]any{"repo": r.Dir, "ref": "main", "path": "dir/t.txt"}, &res)
	if res.Text != "hello\n" || res.Binary || res.Truncated {
		t.Fatalf("text file %+v", res)
	}
	c.ok("fileAt", map[string]any{"repo": r.Dir, "ref": "main", "path": "bin.dat"}, &res)
	if !res.Binary || res.Text != "" {
		t.Fatalf("binary file %+v", res)
	}
	c.ok("fileAt", map[string]any{"repo": r.Dir, "ref": "main", "path": "big.txt"}, &res)
	if !res.Truncated || len(res.Text) != maxText {
		t.Fatalf("big file: truncated=%v len=%d", res.Truncated, len(res.Text))
	}
	if code := c.errCode("fileAt", map[string]any{"repo": r.Dir, "ref": "main", "path": "missing.txt"}); code != "git-failed" {
		t.Fatalf("missing path gave %s", code)
	}
	if code := c.errCode("fileAt", map[string]any{"repo": r.Dir, "ref": "main", "path": "../x"}); code != "bad-params" {
		t.Fatalf("dot-dot path gave %s", code)
	}
}

func TestFileDiffAndTrunkCommits(t *testing.T) {
	r := forkedRepo(t)
	c := startServer(t, r)
	var d struct {
		Diff string `json:"diff"`
	}
	c.ok("fileDiff", map[string]any{"repo": r.Dir, "a": "main", "b": "b", "path": "b1.txt"}, &d)
	if !strings.Contains(d.Diff, "+b1") {
		t.Fatalf("diff %q", d.Diff)
	}
	var list commitList
	c.ok("trunkCommits", map[string]any{"repo": r.Dir, "newer": "b", "older": "main", "limit": 10}, &list)
	if len(list) != 1 || list[0].Subject != "b1" {
		t.Fatalf("newer itself must be dropped: %+v", list)
	}
	c.ok("trunkCommits", map[string]any{"repo": r.Dir, "newer": "main", "older": "main", "limit": 10}, &list)
	if list == nil || len(list) != 0 {
		t.Fatalf("want empty non-nil list, got %#v", list)
	}
}

func TestReposAndGlobal(t *testing.T) {
	r := testrepo.New(t)
	c := startServer(t, r)
	var repos []string
	c.ok("repos", nil, &repos)
	if repos == nil || len(repos) != 0 {
		t.Fatalf("repos %#v", repos)
	}
	c.ok("addRepo", map[string]any{"path": r.Dir}, &repos)
	c.ok("addRepo", map[string]any{"path": r.Dir}, &repos)
	if len(repos) != 1 {
		t.Fatalf("repos %v", repos)
	}
	var d struct {
		Repo string `json:"repo"`
	}
	c.ok("discover", map[string]any{"path": r.Dir}, &d)
	if d.Repo == "" {
		t.Fatal("discover returned nothing")
	}
	c.ok("removeRepo", map[string]any{"repo": r.Dir}, &repos)
	if len(repos) != 0 {
		t.Fatalf("repos after remove %v", repos)
	}
	var g settings.Global
	c.ok("setGlobal", map[string]any{"editor": "cursor", "theme": "dark"}, &g)
	if g.Editor != "cursor" || g.Theme != "dark" {
		t.Fatalf("global %+v", g)
	}
	if code := c.errCode("setGlobal", map[string]any{"editor": "rm -rf"}); code != "bad-params" {
		t.Fatalf("bad editor gave %s", code)
	}
	if code := c.errCode("setGlobal", map[string]any{"theme": "pink"}); code != "bad-params" {
		t.Fatalf("bad theme gave %s", code)
	}
}

func TestNotRepo(t *testing.T) {
	r := testrepo.New(t)
	c := startServer(t, r)
	plain := t.TempDir()
	if code := c.errCode("view", map[string]any{"repo": plain}); code != "not-repo" {
		t.Fatalf("plain folder gave %s", code)
	}
	if code := c.errCode("view", map[string]any{"repo": filepath.Join(plain, "missing")}); code != "not-repo" {
		t.Fatalf("missing folder gave %s", code)
	}
	if code := c.errCode("view", map[string]any{"repo": "relative/path"}); code != "bad-params" {
		t.Fatalf("relative path gave %s", code)
	}
	if code := c.errCode("view", nil); code != "bad-params" {
		t.Fatalf("missing repo gave %s", code)
	}
}

func TestCreateAndRemoveFolder(t *testing.T) {
	r := testrepo.New(t)
	c := startServer(t, r)
	dir := filepath.ToSlash(filepath.Join(r.Root, "wt-feature"))
	var res struct {
		OK   bool   `json:"ok"`
		Path string `json:"path"`
	}
	c.ok("createFolder", map[string]any{"repo": r.Dir, "trunk": "main", "branch": "feature/login", "path": dir}, &res)
	if !res.OK {
		t.Fatalf("createFolder %+v", res)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
	if code := c.errCode("createFolder", map[string]any{"repo": r.Dir, "trunk": "nope", "branch": "feature/x"}); code != "bad-params" {
		t.Fatalf("unknown trunk gave %s", code)
	}
	var v struct {
		Branches map[string]struct {
			Folder     string `json:"folder"`
			FolderHead string `json:"folderHead"`
		} `json:"branches"`
	}
	c.ok("view", map[string]any{"repo": r.Dir}, &v)
	shown := v.Branches["feature/login"]
	if shown.Folder != dir || len(shown.FolderHead) != 40 {
		t.Fatalf("view shows %+v", shown)
	}
	base := map[string]any{"repo": r.Dir, "branch": "feature/login", "folder": shown.Folder, "head": shown.FolderHead}
	for _, drop := range []string{"folder", "head"} {
		p := map[string]any{}
		for k, val := range base {
			if k != drop {
				p[k] = val
			}
		}
		if code := c.errCode("removeFolder", p); code != "bad-params" {
			t.Fatalf("removeFolder without %s gave %s", drop, code)
		}
	}
	var rm struct {
		OK     bool   `json:"ok"`
		Reason string `json:"reason"`
	}
	stale := map[string]any{"repo": r.Dir, "branch": "feature/login", "folder": shown.Folder, "head": strings.Repeat("1", 40)}
	c.ok("removeFolder", stale, &rm)
	if rm.OK || rm.Reason != "changed" {
		t.Fatalf("removeFolder with another head: %+v", rm)
	}
	c.ok("removeFolder", base, &rm)
	if !rm.OK {
		t.Fatalf("removeFolder %+v", rm)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("folder still exists")
	}
	noSuch := map[string]any{"repo": r.Dir, "branch": "no-such", "folder": shown.Folder, "head": shown.FolderHead}
	if code := c.errCode("removeFolder", noSuch); code != "bad-params" {
		t.Fatalf("unknown branch gave %s", code)
	}
	var df struct {
		Path string `json:"path"`
	}
	c.ok("defaultFolder", map[string]any{"repo": r.Dir, "branch": "feature/login"}, &df)
	if !strings.HasSuffix(df.Path, "-wt-login") {
		t.Fatalf("default folder %q", df.Path)
	}
}

func TestRemoteBranchesOpsAndBackups(t *testing.T) {
	r := testrepo.New(t)
	r.Branch("Feature-A", "main")
	r.Push(r.Dir, "origin", "Feature-A")
	r.Git(r.Dir, "fetch", "origin")
	c := startServer(t, r)
	var names []string
	c.ok("remoteBranches", map[string]any{"repo": r.Dir, "query": "feature"}, &names)
	if len(names) != 1 || names[0] != "origin/Feature-A" {
		t.Fatalf("remote branches %v", names)
	}
	c.ok("remoteBranches", map[string]any{"repo": r.Dir, "query": "--no-such"}, &names)
	if names == nil || len(names) != 0 {
		t.Fatalf("want empty non-nil, got %#v", names)
	}
	var entries []settings.OpEntry
	c.ok("ops", map[string]any{"limit": 10}, &entries)
	if entries == nil {
		t.Fatal("ops must be a non-nil list")
	}
	var b struct {
		Dir   string `json:"dir"`
		Bytes int64  `json:"bytes"`
	}
	c.ok("backups", nil, &b)
	if b.Dir == "" || b.Bytes != 0 {
		t.Fatalf("backups %+v", b)
	}
	keep := filepath.Join(c.h.st.Dir, "keep.json")
	os.WriteFile(keep, []byte("{}"), 0o644)
	bk := filepath.Join(c.h.st.Dir, "backups", "k", "f.txt")
	os.MkdirAll(filepath.Dir(bk), 0o755)
	os.WriteFile(bk, []byte("12345"), 0o644)
	c.ok("backups", nil, &b)
	if b.Bytes != 5 {
		t.Fatalf("backups %+v", b)
	}
	c.ok("clearBackups", nil, nil)
	if _, err := os.Stat(bk); err == nil {
		t.Fatal("backup survived clearBackups")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("clearBackups removed more than the backups")
	}
}

func TestErrorCodes(t *testing.T) {
	if e := toRPCError(errors.Join(errors.New("x"), gitx.ErrNotAllowed)); e.Code != "not-allowed" {
		t.Fatalf("%+v", e)
	}
	ge := &gitx.GitError{Args: []string{"log"}, ExitCode: 128, Stderr: "first\nfatal: boom\n\n"}
	if e := toRPCError(ge); e.Code != "git-failed" || e.Message != "fatal: boom" {
		t.Fatalf("%+v", e)
	}
	if e := toRPCError(errors.New("other")); e.Code != "internal" {
		t.Fatalf("%+v", e)
	}
	if e := toRPCError(repo.ErrBareRepo); e.Code != "bare-repo" {
		t.Fatalf("%+v", e)
	}
	unsafe := &repo.UnsafeRepoError{Message: "fatal: detected dubious ownership in repository at 'D:/x'"}
	if e := toRPCError(unsafe); e.Code != "unsafe-repo" || e.Message != unsafe.Message {
		t.Fatalf("%+v", e)
	}
}

func TestBareRepoIsRefused(t *testing.T) {
	r := testrepo.New(t)
	c := startServer(t, r)
	if code := c.errCode("addRepo", map[string]any{"path": r.Remote}); code != "bare-repo" {
		t.Fatalf("bare repository gave %s", code)
	}
}

// editorFixture returns a client whose handler never launches anything: the
// lookup returns lookup and every launch is recorded in *started.
func editorFixture(t *testing.T, lookup string) (*client, *testrepo.Repo, *[][2]string) {
	r := testrepo.New(t)
	c := startServer(t, r)
	var started [][2]string
	c.h.lookPath = func(name string) (string, error) {
		if lookup == "" {
			return "", errors.New("not found")
		}
		return lookup, nil
	}
	c.h.startEditor = func(exe, path string) error {
		started = append(started, [2]string{exe, path})
		return nil
	}
	return c, r, &started
}

func TestOpenInEditorRefusesUnknownPath(t *testing.T) {
	c, r, started := editorFixture(t, "code")
	c.ok("addRepo", map[string]any{"path": r.Dir}, nil)
	outside := t.TempDir()
	for _, p := range []string{outside, filepath.Dir(r.Dir), filepath.Join(r.Dir, "sub"), "relative", ""} {
		if code := c.errCode("openInEditor", map[string]any{"path": p}); code != "bad-params" {
			t.Errorf("path %q gave %s", p, code)
		}
	}
	if len(*started) != 0 {
		t.Fatalf("editor launched for refused paths: %v", *started)
	}
	c.ok("openInEditor", map[string]any{"path": r.Dir}, nil)
	if len(*started) != 1 || (*started)[0][0] != "code" {
		t.Fatalf("started %v", *started)
	}
}

func TestOpenInEditorWorktreeAndNotFound(t *testing.T) {
	c, r, started := editorFixture(t, "code")
	r.Branch("feat", "main")
	wt := r.AddWorktree("one", "feat")
	c.ok("addRepo", map[string]any{"path": r.Dir}, nil)
	c.ok("openInEditor", map[string]any{"path": wt}, nil)
	if len(*started) != 1 || (*started)[0][1] != filepath.FromSlash(wt) {
		t.Fatalf("started %v", *started)
	}
	c.h.lookPath = func(string) (string, error) { return "", errors.New("missing") }
	if code := c.errCode("openInEditor", map[string]any{"path": r.Dir}); code != "editor-not-found" {
		t.Fatalf("missing editor gave %s", code)
	}
}

func TestOpenInEditorRefusesShellCharsForBatchFiles(t *testing.T) {
	c, r, started := editorFixture(t, `C:\tools\code.cmd`)
	bad := filepath.ToSlash(filepath.Join(r.Root, "a&b"))
	r.Git(r.Root, "init", bad)
	c.ok("addRepo", map[string]any{"path": bad}, nil)
	if code := c.errCode("openInEditor", map[string]any{"path": bad}); code != "bad-params" {
		t.Fatalf("batch file with & in path gave %s", code)
	}
	if len(*started) != 0 {
		t.Fatalf("launched: %v", *started)
	}
	// The same path is fine for a real executable.
	c.h.lookPath = func(string) (string, error) { return `C:\tools\code.exe`, nil }
	c.ok("openInEditor", map[string]any{"path": bad}, nil)
	if len(*started) != 1 {
		t.Fatalf("started %v", *started)
	}
	// A plain path is fine for a batch file.
	c.h.lookPath = func(string) (string, error) { return `C:\tools\code.cmd`, nil }
	c.ok("addRepo", map[string]any{"path": r.Dir}, nil)
	c.ok("openInEditor", map[string]any{"path": r.Dir}, nil)
	if len(*started) != 2 {
		t.Fatalf("started %v", *started)
	}
}

func TestShellMetaDetection(t *testing.T) {
	for _, s := range []string{`a"b`, "a&b", "a|b", "a<b", "a>b", "a^b", "a%b", "a!b", "a\nb"} {
		if !hasShellMeta(s) {
			t.Errorf("%q not detected", s)
		}
	}
	if hasShellMeta(`C:/work/my repo (1)`) {
		t.Error("plain path flagged")
	}
}

// createFolder with existing: true checks out a local branch that has no
// folder yet; no trunk is needed and no branch is ever created.
func TestCreateFolderForExistingBranch(t *testing.T) {
	r := testrepo.New(t)
	r.Branch("feature/old", "main")
	c := startServer(t, r)
	dir := filepath.ToSlash(filepath.Join(r.Root, "wt-old"))
	var res struct {
		OK     bool   `json:"ok"`
		Reason string `json:"reason"`
		Path   string `json:"path"`
	}
	c.ok("createFolder", map[string]any{"repo": r.Dir, "branch": "feature/old", "path": dir, "existing": true}, &res)
	if !res.OK || res.Path != dir {
		t.Fatalf("createFolder %+v", res)
	}
	if head := r.Git(dir, "rev-parse", "--symbolic-full-name", "HEAD"); head != "refs/heads/feature/old" {
		t.Fatalf("folder on %s", head)
	}
	c.ok("createFolder", map[string]any{"repo": r.Dir, "branch": "feature/old", "path": dir + "-2", "existing": true}, &res)
	if res.OK || res.Reason != "checked-out-elsewhere" {
		t.Fatalf("second folder: %+v", res)
	}
	c.ok("createFolder", map[string]any{"repo": r.Dir, "branch": "feature/none", "existing": true}, &res)
	if res.OK || res.Reason != "no-branch" {
		t.Fatalf("missing branch: %+v", res)
	}
	if got := r.Git(r.Dir, "for-each-ref", "refs/heads/feature/none"); got != "" {
		t.Fatal("a branch was created")
	}
}
