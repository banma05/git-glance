package repo

import (
	"context"
	"strings"

	"github.com/banma05/git-glance/core/internal/gitx"
)

const maxDirtyFiles = 50

func readDirty(ctx context.Context, r gitx.Runner, dir string) (DirtyCounts, error) {
	out, err := r.Run(ctx, dir, "status", "--porcelain=v2", "-z", "--untracked-files=all")
	if err != nil {
		return DirtyCounts{}, err
	}
	return parseStatus(string(out)), nil
}

// parseStatus reads NUL-separated porcelain v2 entries. A renamed or copied
// entry ("2 ...") is followed by one extra token holding the original path.
func parseStatus(out string) DirtyCounts {
	d := DirtyCounts{Files: []string{}}
	toks := strings.Split(out, "\x00")
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t == "" {
			continue
		}
		var path string
		switch t[0] {
		case '?':
			d.Untracked++
			path = strings.TrimPrefix(t, "? ")
		case '1':
			d.Changed++
			path = entryPath(t, 8)
		case 'u':
			d.Changed++
			path = entryPath(t, 10)
		case '2':
			d.Changed++
			path = entryPath(t, 9)
			i++
		default:
			continue
		}
		if len(d.Files) < maxDirtyFiles {
			d.Files = append(d.Files, path)
		}
	}
	return d
}

// entryPath returns the path after n space-separated fields.
func entryPath(entry string, n int) string {
	parts := strings.SplitN(entry, " ", n+1)
	if len(parts) <= n {
		return ""
	}
	return parts[n]
}
