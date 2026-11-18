// Package repo reads a snapshot of a repository: branches, worktrees and
// their uncommitted changes.
package repo

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/banma05/git-glance/core/internal/gitx"
)

// CommitInfo describes one commit.
type CommitInfo struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
	Time    int64  `json:"time"`
}

// Ref is a local or remote branch. Upstream is the full name of a local
// branch's upstream ("refs/remotes/origin/main", or "refs/heads/main" for a
// local one; "" for none) and UpstreamRemoteRef the branch it follows on that
// remote ("refs/heads/main").
type Ref struct {
	Name              string `json:"name"`
	Hash              string `json:"hash"`
	Upstream          string `json:"upstream"`
	UpstreamRemoteRef string `json:"upstreamRemoteRef"`
	Subject           string `json:"subject"`
	Time              int64  `json:"time"`
	Ahead             int    `json:"ahead"`
	Behind            int    `json:"behind"`
	UpstreamGone      bool   `json:"upstreamGone"`
}

// DirtyCounts summarizes uncommitted changes of one worktree.
type DirtyCounts struct {
	Changed   int      `json:"changed"`
	Untracked int      `json:"untracked"`
	Files     []string `json:"files"` // first 50 paths
}

// Worktree is one checked-out folder of the repository. Locked is set by
// `git worktree lock`; git then refuses to remove the folder.
type Worktree struct {
	Path     string      `json:"path"`
	Branch   string      `json:"branch"`
	Head     string      `json:"head"`
	Main     bool        `json:"main"`
	Detached bool        `json:"detached"`
	Missing  bool        `json:"missing"`
	Locked   bool        `json:"locked"`
	Dirty    DirtyCounts `json:"dirty"`
}

// Snapshot is everything the panel shows about one repository.
type Snapshot struct {
	Repo       string     `json:"repo"`
	Local      []Ref      `json:"local"`
	Remote     []Ref      `json:"remote"`
	Worktrees  []Worktree `json:"worktrees"`
	RemoteHead string     `json:"remoteHead"`
}

// ErrNotRepo is returned by Discover outside of a git repository.
var ErrNotRepo = errors.New("not a git repository")

// Discover returns the main worktree root (absolute, forward slashes) of the
// repository that contains path.
func Discover(ctx context.Context, r gitx.Runner, path string) (string, error) {
	out, err := r.Run(ctx, path, "rev-parse", "--git-common-dir")
	if err != nil {
		var ge *gitx.GitError
		if errors.As(err, &ge) {
			return "", ErrNotRepo
		}
		return "", err
	}
	common := strings.TrimSpace(string(out))
	if !filepath.IsAbs(common) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		common = filepath.Join(abs, common)
	}
	return filepath.ToSlash(filepath.Dir(filepath.Clean(common))), nil
}

// Read takes a snapshot of the repository whose main worktree is repoRoot.
func Read(ctx context.Context, r gitx.Runner, repoRoot string) (Snapshot, error) {
	s := Snapshot{Repo: filepath.ToSlash(repoRoot)}
	var err error
	if s.Local, s.Remote, err = readRefs(ctx, r, repoRoot); err != nil {
		return s, err
	}
	if s.Worktrees, err = readWorktrees(ctx, r, repoRoot); err != nil {
		return s, err
	}
	s.RemoteHead = readRemoteHead(ctx, r, repoRoot)
	return s, nil
}

func readRemoteHead(ctx context.Context, r gitx.Runner, dir string) string {
	out, err := r.Run(ctx, dir, "symbolic-ref", "-q", "--short", "refs/remotes/origin/HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(string(out)), "origin/")
}
