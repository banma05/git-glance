package rpc

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/banma05/git-glance/core/internal/model"
	"github.com/banma05/git-glance/core/internal/ops"
	"github.com/banma05/git-glance/core/internal/repo"
	"github.com/banma05/git-glance/core/internal/settings"
)

type method struct {
	write bool // changes the local machine: takes the global write lock
	fn    func(context.Context, json.RawMessage) (any, error)
}

func (h *Handler) table() map[string]method {
	read := func(fn func(context.Context, json.RawMessage) (any, error)) method { return method{false, fn} }
	write := func(fn func(context.Context, json.RawMessage) (any, error)) method { return method{true, fn} }
	return map[string]method{
		"version":         read(h.version),
		"discover":        read(h.discover),
		"repos":           read(h.repos),
		"addRepo":         write(h.addRepo),
		"removeRepo":      write(h.removeRepo),
		"view":            read(h.view),
		"fetch":           write(h.fetch),
		"align":           write(h.align),
		"switch":          write(h.switchBranch),
		"createFolder":    write(h.createFolder),
		"removeFolder":    write(h.removeFolder),
		"compare":         read(h.compare),
		"fileAt":          read(h.fileAt),
		"fileDiff":        read(h.fileDiff),
		"setRole":         write(h.setRole),
		"moveTrunk":       write(h.moveTrunk),
		"assign":          write(h.assign),
		"setRepoSettings": write(h.setRepoSettings),
		"getGlobal":       read(h.getGlobal),
		"setGlobal":       write(h.setGlobal),
		"ops":             read(h.ops),
		"defaultFolder":   read(h.defaultFolder),
		"remoteBranches":  read(h.remoteBranches),
		"trunkCommits":    read(h.trunkCommits),
		"backups":         read(h.backups),
		"clearBackups":    write(h.clearBackups),
		"openInEditor":    read(h.openInEditor),
	}
}

// decode reads the params of a request. Missing params leave v unchanged.
func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return badParams("params: %v", err)
	}
	return nil
}

// repoRoot resolves a caller-supplied folder to the main worktree root of its
// repository.
func (h *Handler) repoRoot(ctx context.Context, path string) (string, error) {
	if path == "" {
		return "", badParams("repo is required")
	}
	if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return "", badParams("repo must be an absolute folder path")
	}
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		return "", repo.ErrNotRepo
	}
	return repo.Discover(ctx, h.r, path)
}

// isFullHash reports whether s is a full SHA-1 or SHA-256 object name.
func isFullHash(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f') {
			return false
		}
	}
	return true
}

// plainName checks a branch name or similar that ends up in a git argument.
func plainName(label, s string) error {
	if s == "" || strings.HasPrefix(s, "-") || hasControl(s) || len(s) > 1024 {
		return badParams("%s is not a valid name", label)
	}
	return nil
}

// VersionResult is the answer of the version method.
type VersionResult struct {
	Core  string `json:"core"`
	Git   string `json:"git"`
	GitOK bool   `json:"gitOK"`
}

func (h *Handler) version(context.Context, json.RawMessage) (any, error) {
	res := VersionResult{Core: h.coreVersion}
	if h.gitFound {
		res.Git = strconv.Itoa(h.gitVersion.Major) + "." + strconv.Itoa(h.gitVersion.Minor)
		res.GitOK = h.gitVersion.AtLeast(2, 30)
	}
	return res, nil
}

type repoParam struct {
	Repo string `json:"repo"`
}

func (h *Handler) discover(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct{ Path string }
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	root, err := h.repoRoot(ctx, p.Path)
	if err != nil {
		return nil, err
	}
	return struct {
		Repo string `json:"repo"`
	}{root}, nil
}

func (h *Handler) repos(context.Context, json.RawMessage) (any, error) {
	g, err := h.st.Global()
	if err != nil {
		return nil, err
	}
	return g.Repos, nil
}

func (h *Handler) addRepo(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct{ Path string }
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	root, err := h.repoRoot(ctx, p.Path)
	if err != nil {
		return nil, err
	}
	g, err := h.st.Global()
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(g.Repos, func(r string) bool { return sameFolder(r, root) }) {
		g.Repos = append(g.Repos, root)
		if err := h.st.SaveGlobal(g); err != nil {
			return nil, err
		}
	}
	return g.Repos, nil
}

func (h *Handler) removeRepo(_ context.Context, raw json.RawMessage) (any, error) {
	var p repoParam
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if p.Repo == "" || !filepath.IsAbs(p.Repo) {
		return nil, badParams("repo must be an absolute folder path")
	}
	g, err := h.st.Global()
	if err != nil {
		return nil, err
	}
	kept := []string{}
	for _, r := range g.Repos {
		if !sameFolder(r, p.Repo) {
			kept = append(kept, r)
		}
	}
	if len(kept) != len(g.Repos) {
		g.Repos = kept
		if err := h.st.SaveGlobal(g); err != nil {
			return nil, err
		}
	}
	return g.Repos, nil
}

func (h *Handler) noteFetch(root string, res ops.FetchResult) {
	h.fetchMu.Lock()
	defer h.fetchMu.Unlock()
	cur := h.fetched[root]
	if res.OK {
		cur.at = res.At
	}
	cur.err = res.Error
	h.fetched[root] = cur
}

func (h *Handler) view(ctx context.Context, raw json.RawMessage) (any, error) {
	var p repoParam
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	root, err := h.repoRoot(ctx, p.Repo)
	if err != nil {
		return nil, err
	}
	return h.buildView(ctx, root)
}

func (h *Handler) buildView(ctx context.Context, root string) (model.View, error) {
	snap, err := repo.Read(ctx, h.r, root)
	if err != nil {
		return model.View{}, err
	}
	rs, err := h.st.Repo(root)
	if err != nil {
		return model.View{}, err
	}
	v, err := model.Build(ctx, h.r, h.gitVersion, snap, rs, h.now().Unix())
	if err != nil {
		return model.View{}, err
	}
	h.fetchMu.Lock()
	last := h.fetched[root]
	h.fetchMu.Unlock()
	v.FetchedAt, v.FetchError = last.at, last.err
	return v, nil
}

func (h *Handler) fetch(ctx context.Context, raw json.RawMessage) (any, error) {
	var p repoParam
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	root, err := h.repoRoot(ctx, p.Repo)
	if err != nil {
		return nil, err
	}
	res := ops.Fetch(ctx, h.r, root, h.now)
	h.noteFetch(root, res)
	return res, nil
}

func (h *Handler) align(ctx context.Context, raw json.RawMessage) (any, error) {
	var p repoParam
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	root, err := h.repoRoot(ctx, p.Repo)
	if err != nil {
		return nil, err
	}
	rep, err := ops.Align(ctx, h.r, h.st, root, h.now)
	h.noteFetch(root, rep.Fetch)
	if err != nil {
		return nil, err
	}
	return rep, nil
}

func (h *Handler) switchBranch(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct{ Repo, Folder, Branch string }
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if p.Folder == "" || hasControl(p.Folder) {
		return nil, badParams("folder is required")
	}
	if err := plainName("branch", p.Branch); err != nil {
		return nil, err
	}
	root, err := h.repoRoot(ctx, p.Repo)
	if err != nil {
		return nil, err
	}
	res, err := ops.Switch(ctx, h.r, h.st, root, p.Folder, p.Branch, h.now)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// trunkRef resolves a trunk name to the full ref its branches are compared
// through: refs/remotes/<remote>/<name> or refs/heads/<name>.
func (h *Handler) trunkRef(ctx context.Context, root, name string) (string, error) {
	snap, err := repo.Read(ctx, h.r, root)
	if err != nil {
		return "", err
	}
	rs, err := h.st.Repo(root)
	if err != nil {
		return "", err
	}
	for _, t := range model.ResolveTrunks(snap, rs) {
		if t.Name != name {
			continue
		}
		if t.Ref == t.Name {
			return "refs/heads/" + t.Ref, nil
		}
		return "refs/remotes/" + t.Ref, nil
	}
	return "", badParams("unknown trunk %q", name)
}

func (h *Handler) createFolder(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct{ Repo, Trunk, Branch, Path string }
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := plainName("branch", p.Branch); err != nil {
		return nil, err
	}
	if err := plainName("trunk", p.Trunk); err != nil {
		return nil, err
	}
	if p.Path != "" && (!filepath.IsAbs(p.Path) || hasControl(p.Path) || strings.HasPrefix(p.Path, "-")) {
		return nil, badParams("path must be an absolute folder path")
	}
	root, err := h.repoRoot(ctx, p.Repo)
	if err != nil {
		return nil, err
	}
	ref, err := h.trunkRef(ctx, root, p.Trunk)
	if err != nil {
		return nil, err
	}
	dir := p.Path
	if dir == "" {
		dir = ops.DefaultFolderPath(root, p.Branch)
	}
	res, err := ops.CreateFolder(ctx, h.r, h.st, root, p.Trunk, ref, p.Branch, filepath.ToSlash(filepath.Clean(dir)), h.now)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// removeFolder deletes a branch's folder. folder and head are the folder and
// its HEAD hash as the caller showed them; the core refuses with reason
// changed when either is no longer true.
func (h *Handler) removeFolder(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct{ Repo, Branch, Folder, Head string }
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := plainName("branch", p.Branch); err != nil {
		return nil, err
	}
	if p.Folder == "" || !filepath.IsAbs(p.Folder) || hasControl(p.Folder) {
		return nil, badParams("folder must be the absolute path of the branch's folder")
	}
	if !isFullHash(p.Head) {
		return nil, badParams("head must be the full hash of the folder's HEAD")
	}
	root, err := h.repoRoot(ctx, p.Repo)
	if err != nil {
		return nil, err
	}
	// The view is built inside the write lock, right before the removal.
	v, err := h.buildView(ctx, root)
	if err != nil {
		return nil, err
	}
	if _, ok := v.Branches[p.Branch]; !ok {
		return nil, badParams("%s is not a branch of this repository view", p.Branch)
	}
	res, err := ops.RemoveFolder(ctx, h.r, h.st, v, p.Branch, filepath.ToSlash(p.Folder), p.Head, h.now)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (h *Handler) defaultFolder(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct{ Repo, Branch string }
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := plainName("branch", p.Branch); err != nil {
		return nil, err
	}
	root, err := h.repoRoot(ctx, p.Repo)
	if err != nil {
		return nil, err
	}
	return struct {
		Path string `json:"path"`
	}{ops.DefaultFolderPath(root, p.Branch)}, nil
}

func (h *Handler) getGlobal(context.Context, json.RawMessage) (any, error) {
	return h.st.Global()
}

func (h *Handler) setGlobal(_ context.Context, raw json.RawMessage) (any, error) {
	var p struct{ Editor, Theme *string }
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	g, err := h.st.Global()
	if err != nil {
		return nil, err
	}
	if p.Editor != nil {
		if _, ok := editors[*p.Editor]; !ok {
			return nil, badParams("unknown editor %q", *p.Editor)
		}
		g.Editor = *p.Editor
	}
	if p.Theme != nil {
		if !slices.Contains([]string{"auto", "dark", "light"}, *p.Theme) {
			return nil, badParams("unknown theme %q", *p.Theme)
		}
		g.Theme = *p.Theme
	}
	if err := h.st.SaveGlobal(g); err != nil {
		return nil, err
	}
	return g, nil
}

func (h *Handler) ops(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		Repo  string
		Limit int
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 100
	}
	limit = min(limit, 1000)
	root := ""
	if p.Repo != "" {
		r, err := h.repoRoot(ctx, p.Repo)
		switch {
		case err == nil:
			root = r
		case filepath.IsAbs(p.Repo) && !strings.ContainsRune(p.Repo, 0):
			// The repository may be gone; its log entries are still readable.
			root = filepath.ToSlash(filepath.Clean(p.Repo))
		default:
			return nil, err
		}
	}
	return h.st.Ops(root, limit)
}

func (h *Handler) backups(context.Context, json.RawMessage) (any, error) {
	size, err := h.st.BackupsSize()
	if err != nil {
		return nil, err
	}
	return struct {
		Dir   string `json:"dir"`
		Bytes int64  `json:"bytes"`
	}{filepath.Join(h.st.Dir, "backups"), size}, nil
}

func (h *Handler) clearBackups(context.Context, json.RawMessage) (any, error) {
	if err := h.st.ClearBackups(); err != nil {
		return nil, err
	}
	return struct{}{}, nil
}

// editRepoSettings loads the settings of a repository, lets edit change them
// and saves the result. With no trunk configured yet, the trunks the view
// uses today are written out first so that adding one does not drop them.
func (h *Handler) editRepoSettings(ctx context.Context, path string, needTrunks bool, edit func(*settings.RepoSettings) error) (any, error) {
	root, err := h.repoRoot(ctx, path)
	if err != nil {
		return nil, err
	}
	rs, err := h.st.Repo(root)
	if err != nil {
		return nil, err
	}
	if needTrunks && len(rs.Trunks) == 0 {
		snap, err := repo.Read(ctx, h.r, root)
		if err != nil {
			return nil, err
		}
		for _, t := range model.ResolveTrunks(snap, rs) {
			rs.Trunks = append(rs.Trunks, settings.TrunkConfig{Name: t.Name, Releases: []string{}})
		}
	}
	if err := edit(&rs); err != nil {
		return nil, err
	}
	if err := h.st.SaveRepo(root, rs); err != nil {
		return nil, err
	}
	return h.st.Repo(root)
}

func trunkIndex(rs *settings.RepoSettings, name string) int {
	return slices.IndexFunc(rs.Trunks, func(t settings.TrunkConfig) bool { return t.Name == name })
}

// stripRole removes branch from every role it has.
func stripRole(rs *settings.RepoSettings, branch string) {
	if i := trunkIndex(rs, branch); i >= 0 {
		rs.Trunks = slices.Delete(rs.Trunks, i, i+1)
	}
	for i := range rs.Trunks {
		if rs.Trunks[i].Test == branch {
			rs.Trunks[i].Test = ""
		}
		rs.Trunks[i].Releases = slices.DeleteFunc(rs.Trunks[i].Releases, func(s string) bool { return s == branch })
	}
}

func (h *Handler) setRole(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct{ Repo, Branch, Role, Trunk string }
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := plainName("branch", p.Branch); err != nil {
		return nil, err
	}
	if !slices.Contains([]string{"trunk", "test", "release", "none"}, p.Role) {
		return nil, badParams("role must be trunk, test, release or none")
	}
	if (p.Role == "test" || p.Role == "release") && p.Trunk == p.Branch {
		return nil, badParams("a branch cannot be its own trunk's %s branch", p.Role)
	}
	return h.editRepoSettings(ctx, p.Repo, p.Role != "none", func(rs *settings.RepoSettings) error {
		if p.Role == "trunk" && trunkIndex(rs, p.Branch) >= 0 {
			return nil
		}
		stripRole(rs, p.Branch)
		switch p.Role {
		case "trunk":
			rs.Trunks = append(rs.Trunks, settings.TrunkConfig{Name: p.Branch, Releases: []string{}})
		case "test", "release":
			i := trunkIndex(rs, p.Trunk)
			if i < 0 {
				return badParams("unknown trunk %q", p.Trunk)
			}
			if p.Role == "test" {
				rs.Trunks[i].Test = p.Branch
			} else {
				rs.Trunks[i].Releases = append(rs.Trunks[i].Releases, p.Branch)
			}
		}
		return nil
	})
}

func (h *Handler) moveTrunk(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		Repo, Name string
		Index      int
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	return h.editRepoSettings(ctx, p.Repo, true, func(rs *settings.RepoSettings) error {
		i := trunkIndex(rs, p.Name)
		if i < 0 {
			return badParams("unknown trunk %q", p.Name)
		}
		if p.Index < 0 || p.Index >= len(rs.Trunks) {
			return badParams("index %d is out of range", p.Index)
		}
		t := rs.Trunks[i]
		rs.Trunks = slices.Delete(rs.Trunks, i, i+1)
		rs.Trunks = slices.Insert(rs.Trunks, p.Index, t)
		return nil
	})
}

// assign pins a branch to a trunk; an empty trunk removes the pin.
func (h *Handler) assign(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct{ Repo, Branch, Trunk string }
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := plainName("branch", p.Branch); err != nil {
		return nil, err
	}
	return h.editRepoSettings(ctx, p.Repo, true, func(rs *settings.RepoSettings) error {
		if p.Trunk == "" {
			delete(rs.ManualTrunk, p.Branch)
			return nil
		}
		if trunkIndex(rs, p.Trunk) < 0 {
			return badParams("unknown trunk %q", p.Trunk)
		}
		if rs.ManualTrunk == nil {
			rs.ManualTrunk = map[string]string{}
		}
		rs.ManualTrunk[p.Branch] = p.Trunk
		return nil
	})
}

func (h *Handler) setRepoSettings(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		Repo       string
		HidePrefix *string
		OldDays    *int
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if p.HidePrefix != nil && (len(*p.HidePrefix) > 100 || hasControl(*p.HidePrefix)) {
		return nil, badParams("hidePrefix is not valid")
	}
	if p.OldDays != nil && (*p.OldDays < 1 || *p.OldDays > 3650) {
		return nil, badParams("oldDays must be between 1 and 3650")
	}
	return h.editRepoSettings(ctx, p.Repo, false, func(rs *settings.RepoSettings) error {
		if p.HidePrefix != nil {
			rs.HidePrefix = *p.HidePrefix
		}
		if p.OldDays != nil {
			rs.OldDays = *p.OldDays
		}
		return nil
	})
}
