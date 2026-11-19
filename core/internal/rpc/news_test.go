package rpc

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/settings"
	"github.com/banma05/git-glance/core/internal/testrepo"
)

type newsCommit struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
	Time    int64  `json:"time"`
	Author  string `json:"author"`
}

type fileChange struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

type trunkNewsResult struct {
	Commits        []newsCommit `json:"commits"`
	Total          int          `json:"total"`
	Base           string       `json:"base"`
	Tip            string       `json:"tip"`
	Files          []fileChange `json:"files"`
	FilesTruncated bool         `json:"filesTruncated"`
}

type commitFilesResult struct {
	Parent    string       `json:"parent"`
	Files     []fileChange `json:"files"`
	Truncated bool         `json:"truncated"`
}

const (
	newsTrunk  = "refs/remotes/origin/main"
	newsBranch = "refs/heads/feature/login"
)

// newsRepo forks feature/login off main (one own commit by Test, changing
// b.txt), then lands three commits by alice on main (t1..t3, changing a.txt)
// and pushes them: origin/main is three commits ahead of the fork.
func newsRepo(t *testing.T) (*testrepo.Repo, string) {
	t.Helper()
	r := testrepo.New(t)
	fork := r.Git(r.Dir, "rev-parse", "main")
	r.Branch("feature/login", "main")
	r.Git(r.Dir, "checkout", "-q", "feature/login")
	r.Commit(r.Dir, "login", map[string]string{"b.txt": "login\n"})
	r.Git(r.Dir, "checkout", "-q", "main")
	r.Git(r.Dir, "config", "user.name", "alice")
	for i := 1; i <= 3; i++ {
		r.Commit(r.Dir, fmt.Sprintf("t%d", i), map[string]string{"a.txt": fmt.Sprintf("%d\n", i)})
	}
	r.Push(r.Dir, "origin", "main")
	return r, fork
}

// importCommits appends n commits by alice to ref with fast-import, one
// minute apart: subjects c000 (oldest) to c<n-1>, commit i adds f<i>.txt.
func importCommits(t *testing.T, r *testrepo.Repo, ref string, n int) {
	t.Helper()
	var s strings.Builder
	for i := 0; i < n; i++ {
		from := ""
		if i == 0 {
			from = "from " + ref + "^0\n"
		}
		body := fmt.Sprintf("%d\n", i)
		when := 1767300000 + i*60
		fmt.Fprintf(&s, "commit %s\nauthor alice <alice@example.com> %d +0000\ncommitter alice <alice@example.com> %d +0000\n"+
			"data 4\nc%03d\n%sM 644 inline f%d.txt\ndata %d\n%s\n", ref, when, when, i, from, i, len(body), body)
	}
	cmd := exec.Command("git", "fast-import", "--quiet")
	cmd.Dir, cmd.Env, cmd.Stdin = r.Dir, r.Env(), strings.NewReader(s.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fast-import: %v %s", err, out)
	}
}

func subjects(commits []newsCommit) []string {
	s := []string{}
	for _, c := range commits {
		s = append(s, c.Subject)
	}
	return s
}

func fileSet(files []fileChange) map[string]string {
	m := map[string]string{}
	for _, f := range files {
		m[f.Path] = f.Status
	}
	return m
}

func sameFiles(t *testing.T, what string, got []fileChange, want map[string]string) {
	t.Helper()
	m := fileSet(got)
	if len(got) != len(want) || len(m) != len(want) {
		t.Fatalf("%s: files %+v, want %v", what, got, want)
	}
	for p, s := range want {
		if m[p] != s {
			t.Fatalf("%s: files %+v, want %v", what, got, want)
		}
	}
}

// Every place that returns commit info names the commit's author.
func TestCommitInfoHasAuthor(t *testing.T) {
	r, fork := newsRepo(t)
	c := startServer(t, r)
	var cmp struct {
		OnlyA []newsCommit `json:"onlyA"`
		OnlyB []newsCommit `json:"onlyB"`
	}
	c.ok("compare", map[string]any{"repo": r.Dir, "a": newsBranch, "b": newsTrunk}, &cmp)
	if len(cmp.OnlyA) != 1 || cmp.OnlyA[0].Author != "Test" {
		t.Fatalf("compare onlyA %+v, want the login commit by Test", cmp.OnlyA)
	}
	if len(cmp.OnlyB) != 3 {
		t.Fatalf("compare onlyB %+v", cmp.OnlyB)
	}
	for _, x := range cmp.OnlyB {
		if x.Author != "alice" {
			t.Fatalf("compare onlyB %+v, want alice everywhere", cmp.OnlyB)
		}
	}
	rep := c.call("compare", map[string]any{"repo": r.Dir, "a": newsBranch, "b": newsTrunk})
	if !strings.Contains(string(rep.Result), `"author":"alice"`) {
		t.Fatalf("camelCase key author missing: %s", rep.Result)
	}

	var list []newsCommit
	c.ok("trunkCommits", map[string]any{"repo": r.Dir, "newer": newsTrunk, "older": fork, "limit": 10}, &list)
	if len(list) != 2 || list[0].Author != "alice" || list[1].Author != "alice" {
		t.Fatalf("trunkCommits %+v, want t2 and t1 by alice", list)
	}

	var v struct {
		Branches map[string]struct {
			Own []newsCommit `json:"own"`
		} `json:"branches"`
	}
	c.ok("view", map[string]any{"repo": r.Dir}, &v)
	own := v.Branches["feature/login"].Own
	if len(own) != 1 || own[0].Subject != "login" || own[0].Author != "Test" {
		t.Fatalf("view own commits %+v, want the login commit by Test", own)
	}
}

func TestTrunkNewsListsTrunkOnlyCommits(t *testing.T) {
	r, fork := newsRepo(t)
	c := startServer(t, r)
	rep := c.call("trunkNews", map[string]any{"repo": r.Dir, "trunk": newsTrunk, "branch": newsBranch})
	if rep.Error != nil {
		t.Fatalf("trunkNews: %+v", rep.Error)
	}
	for _, key := range []string{`"commits":[`, `"total":3`, `"base":"`, `"tip":"`, `"files":[`, `"filesTruncated":false`} {
		if !strings.Contains(string(rep.Result), key) {
			t.Fatalf("%s missing in %s", key, rep.Result)
		}
	}
	var res trunkNewsResult
	c.ok("trunkNews", map[string]any{"repo": r.Dir, "trunk": newsTrunk, "branch": newsBranch}, &res)
	if got := strings.Join(subjects(res.Commits), ","); got != "t3,t2,t1" {
		t.Fatalf("commits %s, want t3,t2,t1 (newest first)", got)
	}
	for i, x := range res.Commits {
		if x.Author != "alice" || len(x.Hash) != 40 || x.Time == 0 {
			t.Fatalf("commit %d: %+v", i, x)
		}
	}
	if res.Total != 3 {
		t.Fatalf("total %d, want 3", res.Total)
	}
	if res.Base != fork {
		t.Fatalf("base %q, want the fork %q", res.Base, fork)
	}
	if tip := r.Git(r.Dir, "rev-parse", newsTrunk); res.Tip != tip || res.Commits[0].Hash != tip {
		t.Fatalf("tip %q, newest %q, want %q", res.Tip, res.Commits[0].Hash, tip)
	}
	// The branch's own commit is not news, and a branch level with the trunk has none.
	c.ok("trunkNews", map[string]any{"repo": r.Dir, "trunk": newsTrunk, "branch": newsTrunk}, &res)
	if len(res.Commits) != 0 || res.Total != 0 || len(res.Files) != 0 || res.Commits == nil || res.Files == nil {
		t.Fatalf("level branch: %+v", res)
	}
}

// A branch far behind sees the newest 200 commits and the true total.
func TestTrunkNewsCapsListKeepsTotal(t *testing.T) {
	r := testrepo.New(t)
	r.Branch("feature/login", "main")
	importCommits(t, r, "refs/heads/main", maxCommits+5)
	c := startServer(t, r)
	var res trunkNewsResult
	c.ok("trunkNews", map[string]any{"repo": r.Dir, "trunk": "refs/heads/main", "branch": newsBranch}, &res)
	if len(res.Commits) != maxCommits || res.Total != maxCommits+5 {
		t.Fatalf("listed %d, total %d; want %d and %d", len(res.Commits), res.Total, maxCommits, maxCommits+5)
	}
	newest, oldest := fmt.Sprintf("c%03d", maxCommits+4), "c005"
	if res.Commits[0].Subject != newest || res.Commits[maxCommits-1].Subject != oldest {
		t.Fatalf("listed %s..%s, want %s..%s", res.Commits[0].Subject, res.Commits[maxCommits-1].Subject, newest, oldest)
	}
	if len(res.Files) != maxCommits+5 || res.FilesTruncated {
		t.Fatalf("files %d (truncated %v), want %d", len(res.Files), res.FilesTruncated, maxCommits+5)
	}
}

// The combined files are merge-base..trunk tip: the branch's own changes are
// not listed (a two-dot diff of branch and trunk would list b.txt too).
func TestTrunkNewsFilesFromMergeBase(t *testing.T) {
	r, _ := newsRepo(t)
	c := startServer(t, r)
	var cmp struct {
		Files []fileChange `json:"files"`
	}
	c.ok("compare", map[string]any{"repo": r.Dir, "a": newsBranch, "b": newsTrunk}, &cmp)
	if _, ok := fileSet(cmp.Files)["b.txt"]; !ok {
		t.Fatalf("fixture: a two-dot diff should list b.txt: %+v", cmp.Files)
	}
	var res trunkNewsResult
	c.ok("trunkNews", map[string]any{"repo": r.Dir, "trunk": newsTrunk, "branch": newsBranch}, &res)
	sameFiles(t, "trunkNews", res.Files, map[string]string{"a.txt": "A"})
	if res.FilesTruncated {
		t.Fatal("filesTruncated set for one file")
	}
	// A file opens through the existing fileDiff with full hashes base and tip.
	var d struct {
		Diff string `json:"diff"`
	}
	c.ok("fileDiff", map[string]any{"repo": r.Dir, "a": res.Base, "b": res.Tip, "path": "a.txt"}, &d)
	if !strings.Contains(d.Diff, "+3") || strings.Contains(d.Diff, "b.txt") {
		t.Fatalf("fileDiff(base, tip) %q", d.Diff)
	}
}

// A repository without a remote: the trunk is a local branch.
func TestTrunkNewsLocalOnlyTrunk(t *testing.T) {
	r := testrepo.New(t)
	r.Git(r.Dir, "remote", "remove", "origin")
	if refs := r.Git(r.Dir, "for-each-ref", "refs/remotes"); refs != "" {
		t.Fatalf("fixture: remote refs left: %s", refs)
	}
	fork := r.Git(r.Dir, "rev-parse", "main")
	r.Branch("feature/login", "main")
	r.Git(r.Dir, "checkout", "-q", "feature/login")
	r.Commit(r.Dir, "login", map[string]string{"b.txt": "login\n"})
	r.Git(r.Dir, "checkout", "-q", "main")
	r.Commit(r.Dir, "m1", map[string]string{"a.txt": "a\n"})
	r.Commit(r.Dir, "m2", map[string]string{"c.txt": "c\n"})
	c := startServer(t, r)
	var res trunkNewsResult
	c.ok("trunkNews", map[string]any{"repo": r.Dir, "trunk": "refs/heads/main", "branch": newsBranch}, &res)
	if got := strings.Join(subjects(res.Commits), ","); got != "m2,m1" || res.Total != 2 || res.Base != fork {
		t.Fatalf("commits %s total %d base %q, want m2,m1 / 2 / %q", got, res.Total, res.Base, fork)
	}
	sameFiles(t, "trunkNews", res.Files, map[string]string{"a.txt": "A", "c.txt": "A"})
	var list []newsCommit
	c.ok("recentCommits", map[string]any{"repo": r.Dir, "ref": "refs/heads/main", "limit": 0}, &list)
	if got := strings.Join(subjects(list), ","); got != "m2,m1,init" {
		t.Fatalf("recentCommits %s, want m2,m1,init", got)
	}
}

// Histories with no common commit: every trunk commit is news, and there is
// no fork to list combined files from.
func TestTrunkNewsUnrelatedHistories(t *testing.T) {
	r := testrepo.New(t)
	r.Commit(r.Dir, "m1", map[string]string{"a.txt": "a\n"})
	r.Git(r.Dir, "checkout", "-q", "--orphan", "v2")
	r.Commit(r.Dir, "v2 root", map[string]string{"v.txt": "v\n"})
	r.Git(r.Dir, "checkout", "-q", "main")
	c := startServer(t, r)
	rep := c.call("trunkNews", map[string]any{"repo": r.Dir, "trunk": "refs/heads/main", "branch": "refs/heads/v2"})
	if rep.Error != nil {
		t.Fatalf("trunkNews: %+v", rep.Error)
	}
	for _, key := range []string{`"base":""`, `"files":[]`, `"total":2`} {
		if !strings.Contains(string(rep.Result), key) {
			t.Fatalf("%s missing in %s", key, rep.Result)
		}
	}
	var res trunkNewsResult
	c.ok("trunkNews", map[string]any{"repo": r.Dir, "trunk": "refs/heads/main", "branch": "refs/heads/v2"}, &res)
	if got := strings.Join(subjects(res.Commits), ","); got != "m1,init" {
		t.Fatalf("commits %s, want m1,init", got)
	}
}

func TestRecentCommitsLimits(t *testing.T) {
	r := testrepo.New(t)
	importCommits(t, r, "refs/heads/main", 250)
	tip := r.Git(r.Dir, "rev-parse", "main")
	c := startServer(t, r)
	for _, tc := range []struct{ limit, want int }{{0, 50}, {300, maxCommits}, {maxCommits, maxCommits}, {7, 7}, {1, 1}} {
		var list []newsCommit
		c.ok("recentCommits", map[string]any{"repo": r.Dir, "ref": "refs/heads/main", "limit": tc.limit}, &list)
		if len(list) != tc.want {
			t.Fatalf("limit %d: %d commits, want %d", tc.limit, len(list), tc.want)
		}
		if list[0].Hash != tip || list[0].Subject != "c249" || list[0].Author != "alice" {
			t.Fatalf("limit %d: newest %+v, want c249 by alice at %s", tc.limit, list[0], tip)
		}
		for i := 1; i < len(list); i++ {
			if list[i].Time > list[i-1].Time {
				t.Fatalf("limit %d: not newest first at %d", tc.limit, i)
			}
		}
	}
	if code := c.errCode("recentCommits", map[string]any{"repo": r.Dir, "ref": "refs/heads/main", "limit": -1}); code != "bad-params" {
		t.Fatalf("limit -1 gave %s, want bad-params", code)
	}
	if code := c.errCode("recentCommits", map[string]any{"repo": r.Dir, "ref": "refs/heads/no-such", "limit": 5}); code != "bad-params" {
		t.Fatalf("unknown ref gave %s, want bad-params", code)
	}
}

// A merge lists what it brought into the trunk: its change against the first
// parent, not against the merged branch.
func TestCommitFilesMergeUsesFirstParent(t *testing.T) {
	r := testrepo.New(t)
	r.Branch("feature/login", "main")
	r.Git(r.Dir, "checkout", "-q", "feature/login")
	r.Commit(r.Dir, "login", map[string]string{"b.txt": "b\n"})
	r.Commit(r.Dir, "login 2", map[string]string{"c.txt": "c\n"})
	r.Git(r.Dir, "checkout", "-q", "main")
	r.Commit(r.Dir, "m1", map[string]string{"a.txt": "a\n"})
	r.Git(r.Dir, "merge", "--no-ff", "-m", "Merge feature/login", "feature/login")
	merge := r.Git(r.Dir, "rev-parse", "HEAD")
	first := r.Git(r.Dir, "rev-parse", "HEAD^1")
	c := startServer(t, r)
	rep := c.call("commitFiles", map[string]any{"repo": r.Dir, "hash": merge})
	if rep.Error != nil {
		t.Fatalf("commitFiles: %+v", rep.Error)
	}
	for _, key := range []string{`"parent":"`, `"files":[`, `"truncated":false`} {
		if !strings.Contains(string(rep.Result), key) {
			t.Fatalf("%s missing in %s", key, rep.Result)
		}
	}
	var res commitFilesResult
	c.ok("commitFiles", map[string]any{"repo": r.Dir, "hash": merge}, &res)
	if res.Parent != first {
		t.Fatalf("parent %q, want the first parent %q", res.Parent, first)
	}
	sameFiles(t, "merge", res.Files, map[string]string{"b.txt": "A", "c.txt": "A"})
	// An ordinary commit lists its own change.
	c.ok("commitFiles", map[string]any{"repo": r.Dir, "hash": first}, &res)
	sameFiles(t, "m1", res.Files, map[string]string{"a.txt": "A"})
}

func TestCommitFilesRootAndOddPaths(t *testing.T) {
	r := testrepo.New(t)
	root := r.Git(r.Dir, "rev-parse", "main")
	r.Git(r.Dir, "checkout", "-q", "--orphan", "v2")
	v2root := r.Commit(r.Dir, "v2 root", map[string]string{"x.txt": "x\n", "dir/y.txt": "y\n"})
	r.Git(r.Dir, "checkout", "-q", "main")
	odd := "dir with space/文件 1.txt"
	first := r.Commit(r.Dir, "add odd", map[string]string{odd: "one\n"})
	second := r.Commit(r.Dir, "change odd", map[string]string{odd: "two\n"})
	c := startServer(t, r)

	var res commitFilesResult
	c.ok("commitFiles", map[string]any{"repo": r.Dir, "hash": root}, &res)
	if res.Parent != "" {
		t.Fatalf("root parent %q, want empty", res.Parent)
	}
	sameFiles(t, "root", res.Files, map[string]string{"README": "A"})
	c.ok("commitFiles", map[string]any{"repo": r.Dir, "hash": v2root}, &res)
	if res.Parent != "" {
		t.Fatalf("v2 root parent %q, want empty", res.Parent)
	}
	sameFiles(t, "v2 root", res.Files, map[string]string{"README": "A", "x.txt": "A", "dir/y.txt": "A"})

	c.ok("commitFiles", map[string]any{"repo": r.Dir, "hash": second}, &res)
	if res.Parent != first {
		t.Fatalf("parent %q, want %q", res.Parent, first)
	}
	sameFiles(t, "odd path", res.Files, map[string]string{odd: "M"})
	var d struct {
		Diff string `json:"diff"`
	}
	c.ok("fileDiff", map[string]any{"repo": r.Dir, "a": res.Parent, "b": second, "path": res.Files[0].Path}, &d)
	if !strings.Contains(d.Diff, "-one") || !strings.Contains(d.Diff, "+two") {
		t.Fatalf("fileDiff(parent, hash) %q", d.Diff)
	}
	if code := c.errCode("commitFiles", map[string]any{"repo": r.Dir, "hash": strings.Repeat("0", 40)}); code != "bad-params" {
		t.Fatalf("unknown hash gave %s, want bad-params", code)
	}
}

// The empty tree a root commit is listed against follows the repository's
// object format.
func TestCommitFilesRootSHA256(t *testing.T) {
	r := testrepo.New(t)
	dir := r.Root + "/sha256"
	cmd := exec.Command("git", "init", "-q", "--object-format=sha256", dir)
	cmd.Env = r.Env()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("this git cannot make a SHA-256 repository: %v %s", err, out)
	}
	h := r.Commit(dir, "root", map[string]string{"x.txt": "x\n"})
	if len(h) != 64 {
		t.Fatalf("fixture: hash %q is not SHA-256", h)
	}
	c := startServer(t, r)
	var res commitFilesResult
	c.ok("commitFiles", map[string]any{"repo": dir, "hash": h}, &res)
	if res.Parent != "" {
		t.Fatalf("parent %q, want empty", res.Parent)
	}
	sameFiles(t, "sha256 root", res.Files, map[string]string{"x.txt": "A"})
}

// The news methods are reads: not in the write set, taking a read slot, and
// running only read-only git commands.
func TestNewsMethodsAreReads(t *testing.T) {
	h, err := NewHandler(gitx.ExecRunner{}, settings.Store{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"trunkNews", "recentCommits", "commitFiles"} {
		m, ok := h.methods[name]
		if !ok || m.write || m.noGit {
			t.Errorf("method %s: present %v, write %v, noGit %v; want a read", name, ok, m.write, m.noGit)
		}
	}

	r, _ := newsRepo(t)
	rec := newRecRunner()
	c := startServerWith(t, r, rec)
	rec.reset()
	c.ok("trunkNews", map[string]any{"repo": r.Dir, "trunk": newsTrunk, "branch": newsBranch}, nil)
	c.ok("recentCommits", map[string]any{"repo": r.Dir, "ref": newsTrunk, "limit": 5}, nil)
	c.ok("commitFiles", map[string]any{"repo": r.Dir, "hash": r.Git(r.Dir, "rev-parse", "main")}, nil)
	reads := map[string]bool{"rev-parse": true, "log": true, "diff": true, "merge-base": true, "rev-list": true}
	calls := rec.snapshot()
	if len(calls) == 0 {
		t.Fatal("no git call was recorded, the check is empty")
	}
	for _, args := range calls {
		if !reads[args[0]] && !slices.Equal(args, []string{"worktree", "list", "--porcelain"}) {
			t.Errorf("a news method ran %q", args)
		}
	}
}
