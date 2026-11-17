package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/repo"
)

const (
	maxCommits = 200
	maxFiles   = 2000
	maxText    = 1 << 20 // longest file text or diff returned
	maxRemote  = 50

	// Every log and show call carries noSignature (a user's log.showSignature
	// would add lines to parsed output); every diff and show call carries the
	// diff options so no external program or text filter runs.
	noSignature = "--no-show-signature"
	logFormat   = "--format=%H%x00%s%x00%ct"
	literalSpec = ":(literal)"
)

var diffOptions = []string{"--no-ext-diff", "--no-textconv", "--no-color"}

var commitHash = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// resolveRev turns a caller-supplied revision into a commit hash. The caller's
// text never reaches another git command.
func (h *Handler) resolveRev(ctx context.Context, root, label, rev string) (string, error) {
	if rev == "" || strings.HasPrefix(rev, "-") || hasControl(rev) {
		return "", badParams("%s is not a valid revision", label)
	}
	out, err := h.r.Run(ctx, root, "rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	var ge *gitx.GitError
	if errors.As(err, &ge) && ge.ExitCode == 1 {
		return "", badParams("%s: unknown revision", label)
	}
	if err != nil {
		return "", err
	}
	hash := strings.TrimSpace(string(out))
	if !commitHash.MatchString(hash) {
		return "", errors.New("git returned an unexpected hash")
	}
	return hash, nil
}

// pathInTree checks a path that is joined to a revision as <rev>:<path>.
func pathInTree(p string) error {
	if p == "" || hasControl(p) || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") || strings.HasPrefix(p, ":") {
		return badParams("path is not a valid repository path")
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." || part == "." {
			return badParams("path is not a valid repository path")
		}
	}
	return nil
}

func parseCommits(out []byte) ([]repo.CommitInfo, error) {
	commits := []repo.CommitInfo{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(strings.TrimRight(line, "\r"), "\x00")
		if len(f) != 3 || f[0] == "" {
			continue
		}
		ts, err := strconv.ParseInt(f[2], 10, 64)
		if err != nil {
			return nil, err
		}
		commits = append(commits, repo.CommitInfo{Hash: f[0], Subject: f[1], Time: ts})
	}
	return commits, nil
}

func (h *Handler) commitsBetween(ctx context.Context, root, from, to string, n int) ([]repo.CommitInfo, error) {
	out, err := h.r.Run(ctx, root, "log", noSignature, logFormat, "-n", strconv.Itoa(n), from+".."+to, "--")
	if err != nil {
		return nil, err
	}
	return parseCommits(out)
}

// FileChange is one entry of the file list of a comparison.
type FileChange struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

// CompareResult holds the commits only a has, the commits only b has (each at
// most 200, newest first) and the files that differ between a and b.
type CompareResult struct {
	OnlyA          []repo.CommitInfo `json:"onlyA"`
	OnlyB          []repo.CommitInfo `json:"onlyB"`
	Files          []FileChange      `json:"files"`
	FilesTruncated bool              `json:"filesTruncated"`
}

func (h *Handler) compare(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct{ Repo, A, B string }
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	root, err := h.repoRoot(ctx, p.Repo)
	if err != nil {
		return nil, err
	}
	a, err := h.resolveRev(ctx, root, "a", p.A)
	if err != nil {
		return nil, err
	}
	b, err := h.resolveRev(ctx, root, "b", p.B)
	if err != nil {
		return nil, err
	}
	res := CompareResult{}
	if res.OnlyA, err = h.commitsBetween(ctx, root, b, a, maxCommits); err != nil {
		return nil, err
	}
	if res.OnlyB, err = h.commitsBetween(ctx, root, a, b, maxCommits); err != nil {
		return nil, err
	}
	args := append([]string{"diff", "--name-status", "-z", "--no-renames"}, diffOptions...)
	out, err := h.r.Run(ctx, root, append(args, a, b, "--")...)
	if err != nil {
		return nil, err
	}
	res.Files, res.FilesTruncated = parseNameStatus(out)
	return res, nil
}

// parseNameStatus reads `diff --name-status -z --no-renames` output:
// status NUL path NUL, repeated.
func parseNameStatus(out []byte) ([]FileChange, bool) {
	files := []FileChange{}
	parts := strings.Split(string(out), "\x00")
	for i := 0; i+1 < len(parts); i += 2 {
		if len(files) == maxFiles {
			return files, true
		}
		files = append(files, FileChange{Path: parts[i+1], Status: parts[i][:min(1, len(parts[i]))]})
	}
	return files, false
}

// FileText is the text of a file at a revision, cut at 1 MiB.
type FileText struct {
	Text      string `json:"text"`
	Binary    bool   `json:"binary"`
	Truncated bool   `json:"truncated"`
}

func (h *Handler) fileAt(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct{ Repo, Ref, Path string }
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := pathInTree(p.Path); err != nil {
		return nil, err
	}
	root, err := h.repoRoot(ctx, p.Repo)
	if err != nil {
		return nil, err
	}
	hash, err := h.resolveRev(ctx, root, "ref", p.Ref)
	if err != nil {
		return nil, err
	}
	args := append([]string{"show", noSignature}, diffOptions...)
	out, err := h.r.Run(ctx, root, append(args, "--end-of-options", hash+":"+p.Path)...)
	if err != nil {
		return nil, err
	}
	if strings.IndexByte(string(out), 0) >= 0 {
		return FileText{Binary: true}, nil
	}
	return capText(out, FileText{}), nil
}

func capText(out []byte, res FileText) FileText {
	if len(out) > maxText {
		cut := maxText
		for cut > 0 && !utf8.RuneStart(out[cut]) {
			cut-- // do not split a character
		}
		out = out[:cut]
		res.Truncated = true
	}
	res.Text = string(out)
	return res
}

func (h *Handler) fileDiff(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct{ Repo, A, B, Path string }
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if p.Path == "" || hasControl(p.Path) {
		return nil, badParams("path is not a valid repository path")
	}
	root, err := h.repoRoot(ctx, p.Repo)
	if err != nil {
		return nil, err
	}
	a, err := h.resolveRev(ctx, root, "a", p.A)
	if err != nil {
		return nil, err
	}
	b, err := h.resolveRev(ctx, root, "b", p.B)
	if err != nil {
		return nil, err
	}
	args := append([]string{"diff"}, diffOptions...)
	out, err := h.r.Run(ctx, root, append(args, a, b, "--", literalSpec+p.Path)...)
	if err != nil {
		return nil, err
	}
	t := capText(out, FileText{})
	return struct {
		Diff      string `json:"diff"`
		Truncated bool   `json:"truncated"`
	}{t.Text, t.Truncated}, nil
}

// trunkCommits lists the commits in older..newer without newer itself, newest
// first, at most limit of them.
func (h *Handler) trunkCommits(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		Repo, Newer, Older string
		Limit              int
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	limit := p.Limit
	if limit < 0 {
		return nil, badParams("limit must not be negative")
	}
	if limit == 0 || limit > maxCommits {
		limit = maxCommits
	}
	root, err := h.repoRoot(ctx, p.Repo)
	if err != nil {
		return nil, err
	}
	newer, err := h.resolveRev(ctx, root, "newer", p.Newer)
	if err != nil {
		return nil, err
	}
	older, err := h.resolveRev(ctx, root, "older", p.Older)
	if err != nil {
		return nil, err
	}
	commits, err := h.commitsBetween(ctx, root, older, newer, limit+1)
	if err != nil {
		return nil, err
	}
	if len(commits) > 0 && commits[0].Hash == newer {
		commits = commits[1:]
	}
	if len(commits) > limit {
		commits = commits[:limit]
	}
	return commits, nil
}

// remoteBranches lists remote branch names containing query, ignoring case.
// The query is matched here and never given to git.
func (h *Handler) remoteBranches(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct{ Repo, Query string }
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	root, err := h.repoRoot(ctx, p.Repo)
	if err != nil {
		return nil, err
	}
	snap, err := repo.Read(ctx, h.r, root)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(p.Query)
	names := []string{}
	for _, r := range snap.Remote {
		if strings.Contains(strings.ToLower(r.Name), q) {
			names = append(names, r.Name)
		}
	}
	sort.Strings(names)
	if len(names) > maxRemote {
		names = names[:maxRemote]
	}
	return names, nil
}
