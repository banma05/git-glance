package rpc

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/banma05/git-glance/core/internal/repo"
)

// editors maps the names allowed in Global.Editor to the executable looked up
// on PATH.
var editors = map[string]string{
	"code": "code", "cursor": "cursor", "windsurf": "windsurf",
	"idea": "idea", "pycharm": "pycharm", "webstorm": "webstorm", "goland": "goland",
}

// lookPathOutsideCwd is exec.LookPath that refuses a result found relative to
// the current folder.
func lookPathOutsideCwd(name string) (string, error) {
	p, err := exec.LookPath(name)
	if err != nil { // includes exec.ErrDot
		return "", err
	}
	return p, nil
}

// launchEditor starts exe with the single argument path, without a shell, and
// reaps the process in the background.
func launchEditor(exe, path string) error {
	cmd := exec.Command(exe, path)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// hasShellMeta reports whether cmd.exe could treat part of s as syntax when
// it re-parses the arguments of a batch file.
func hasShellMeta(s string) bool {
	return strings.ContainsAny(s, "\"&|<>^%!\r\n")
}

func isBatchFile(exe string) bool {
	ext := strings.ToLower(filepath.Ext(exe))
	return ext == ".cmd" || ext == ".bat"
}

// sameFolder compares cleaned paths, ignoring case on Windows.
func sameFolder(a, b string) bool {
	a, b = filepath.ToSlash(filepath.Clean(a)), filepath.ToSlash(filepath.Clean(b))
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// knownFolders lists the root and the worktree folders of every repository in
// the global list.
func (h *Handler) knownFolders(ctx context.Context) ([]string, error) {
	g, err := h.st.Global()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, root := range g.Repos {
		out = append(out, root)
		snap, err := repo.Read(ctx, h.r, root)
		if err != nil {
			continue // a repository that moved away has no folders to offer
		}
		for _, w := range snap.Worktrees {
			out = append(out, w.Path)
		}
	}
	return out, nil
}

// openInEditor opens a repository root or one of its worktree folders in the
// editor chosen in the global settings. Nothing else can be opened.
func (h *Handler) openInEditor(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if p.Path == "" || !filepath.IsAbs(p.Path) || strings.ContainsRune(p.Path, 0) {
		return nil, badParams("path must be an absolute folder path")
	}
	folders, err := h.knownFolders(ctx)
	if err != nil {
		return nil, err
	}
	known := false
	for _, f := range folders {
		if sameFolder(f, p.Path) {
			known = true
			break
		}
	}
	if !known {
		return nil, badParams("path is not a repository or worktree folder of a known repository")
	}
	g, err := h.st.Global()
	if err != nil {
		return nil, err
	}
	name, ok := editors[g.Editor]
	if !ok {
		return nil, badParams("unknown editor %q", g.Editor)
	}
	exe, err := h.lookPath(name)
	if err != nil {
		return nil, &RPCError{Code: "editor-not-found", Message: "cannot find " + name + " on PATH"}
	}
	path := filepath.FromSlash(filepath.Clean(p.Path))
	if isBatchFile(exe) && hasShellMeta(path) {
		return nil, badParams("path contains characters a batch file would misread")
	}
	if err := h.startEditor(exe, path); err != nil {
		return nil, err
	}
	return struct{}{}, nil
}
