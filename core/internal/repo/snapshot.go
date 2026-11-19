// Package repo reads a snapshot of a repository: branches, worktrees and
// their uncommitted changes.
package repo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/banma05/git-glance/core/internal/gitx"
)

// CommitInfo describes one commit. Author is the author's name.
type CommitInfo struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
	Time    int64  `json:"time"`
	Author  string `json:"author"`
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
// `git worktree lock`; git then refuses to remove the folder. Broken means
// the folder exists but git cannot read it; it is also Missing, so nothing
// touches it.
type Worktree struct {
	Path     string      `json:"path"`
	Branch   string      `json:"branch"`
	Head     string      `json:"head"`
	Main     bool        `json:"main"`
	Detached bool        `json:"detached"`
	Missing  bool        `json:"missing"`
	Broken   bool        `json:"broken"`
	Locked   bool        `json:"locked"`
	Bare     bool        `json:"bare"`
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

// ErrBareRepo is returned by Discover for a bare repository, which has no
// folders to show.
var ErrBareRepo = errors.New("bare repositories are not supported")

// UnsafeRepoError is returned by Discover when git refuses a repository that
// belongs to another user ("dubious ownership"). Message is git's text, which
// says how to trust the folder.
type UnsafeRepoError struct{ Message string }

func (e *UnsafeRepoError) Error() string { return e.Message }

// Discover returns the main worktree root (absolute, forward slashes) of the
// repository that contains path: the first folder git lists for it, from any
// of its folders. A submodule is its own repository, whose git directory lies
// inside the superproject's.
func Discover(ctx context.Context, r gitx.Runner, path string) (string, error) {
	list, err := listWorktrees(ctx, r, path)
	if err != nil {
		var ge *gitx.GitError
		if !errors.As(err, &ge) {
			return "", err
		}
		if strings.Contains(ge.Stderr, "dubious ownership") {
			return "", &UnsafeRepoError{Message: strings.TrimSpace(ge.Stderr)}
		}
		return "", ErrNotRepo
	}
	switch {
	case len(list) == 0:
		return "", ErrNotRepo
	case list[0].Bare:
		return "", ErrBareRepo
	}
	return filepath.ToSlash(filepath.Clean(list[0].Path)), nil
}

// listWorktrees lists the folders of the repository that contains dir, the
// main folder first. Discover and Read both use it, so they agree on that
// folder.
func listWorktrees(ctx context.Context, r gitx.Runner, dir string) ([]Worktree, error) {
	out, err := r.Run(ctx, dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	list := parseWorktrees(string(out))
	if len(list) > 0 && !list[0].Bare {
		list[0].Path = mainFolder(ctx, r, list[0].Path)
	}
	return list, nil
}

// ListFolders lists the folders of the repository whose main folder is
// repoRoot, the main folder first, without reading their changes (Dirty and
// Missing are not set).
func ListFolders(ctx context.Context, r gitx.Runner, repoRoot string) ([]Worktree, error) {
	return listWorktrees(ctx, r, repoRoot)
}

// mainFolder corrects the first folder git lists: for a submodule git lists
// its git directory (.git/modules/<name>), while the folder itself is set by
// core.worktree there. Asked inside that directory, git names the folder. A
// folder that holds a .git entry is a working folder and needs no question.
func mainFolder(ctx context.Context, r gitx.Runner, listed string) string {
	if _, err := os.Lstat(filepath.Join(listed, ".git")); err == nil {
		return listed
	}
	out, err := r.Run(ctx, listed, "rev-parse", "--is-inside-git-dir")
	if err != nil || strings.TrimSpace(string(out)) != "true" {
		return listed
	}
	out, err = r.Run(ctx, listed, "rev-parse", "--show-toplevel")
	if top := strings.TrimSpace(string(out)); err == nil && top != "" {
		return top
	}
	return listed
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
