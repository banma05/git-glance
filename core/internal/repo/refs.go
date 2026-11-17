package repo

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/banma05/git-glance/core/internal/gitx"
)

const refFormat = "%(refname)%00%(objectname)%00%(upstream:short)%00" +
	"%(upstream:track)%00%(committerdate:unix)%00%(subject)"

func readRefs(ctx context.Context, r gitx.Runner, dir string) (local, remote []Ref, err error) {
	out, err := r.Run(ctx, dir, "for-each-ref", "--format="+refFormat, "refs/heads", "refs/remotes")
	if err != nil {
		return nil, nil, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		ref, full, err := parseRef(line)
		if err != nil {
			return nil, nil, err
		}
		switch {
		case strings.HasPrefix(full, "refs/heads/"):
			ref.Name = strings.TrimPrefix(full, "refs/heads/")
			local = append(local, ref)
		case strings.HasPrefix(full, "refs/remotes/"):
			ref.Name = strings.TrimPrefix(full, "refs/remotes/")
			if strings.HasSuffix(ref.Name, "/HEAD") {
				continue
			}
			remote = append(remote, ref)
		}
	}
	return local, remote, nil
}

func parseRef(line string) (Ref, string, error) {
	f := strings.SplitN(line, "\x00", 6)
	if len(f) != 6 {
		return Ref{}, "", fmt.Errorf("unexpected for-each-ref line %q", line)
	}
	ref := Ref{Hash: f[1], Upstream: f[2], Subject: f[5]}
	ref.Time, _ = strconv.ParseInt(f[4], 10, 64)
	ref.Ahead, ref.Behind, ref.UpstreamGone = parseTrack(f[3])
	return ref, f[0], nil
}

// parseTrack reads "[ahead N, behind M]", "[ahead N]", "[behind M]", "[gone]".
func parseTrack(s string) (ahead, behind int, gone bool) {
	s = strings.Trim(strings.TrimSpace(s), "[]")
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		switch {
		case part == "gone":
			gone = true
		case strings.HasPrefix(part, "ahead "):
			ahead, _ = strconv.Atoi(strings.TrimPrefix(part, "ahead "))
		case strings.HasPrefix(part, "behind "):
			behind, _ = strconv.Atoi(strings.TrimPrefix(part, "behind "))
		}
	}
	return ahead, behind, gone
}
