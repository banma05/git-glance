package model

import (
	"context"
	"strconv"
	"strings"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/repo"
)

const createdFrom = "branch: Created from "

// recordChunk caps the refs of one log call, to keep the command line well
// under the Windows limit.
const recordChunk = 200

// creationRecords reads git's own note of where local branches were created:
// per branch the start named by its oldest reflog entry, when that entry says
// "branch: Created from <start>". Anything else (no reflog, an expired
// creation entry, a failed or cancelled read) leaves the branch out; a failed
// call costs only its own chunk.
func creationRecords(ctx context.Context, r gitx.Runner, dir string, branches []string) map[string]string {
	out := map[string]string{}
	for len(branches) > 0 {
		n := min(recordChunk, len(branches))
		for branch, start := range readRecords(ctx, r, dir, branches[:n]) {
			out[branch] = start
		}
		branches = branches[n:]
	}
	return out
}

func readRecords(ctx context.Context, r gitx.Runner, dir string, branches []string) map[string]string {
	args := []string{"log", "--walk-reflogs", noSignature, "--format=%gD%x00%gs"}
	for _, b := range branches {
		args = append(args, "refs/heads/"+b)
	}
	data, err := r.Run(ctx, dir, append(args, "--")...)
	if err != nil {
		return nil
	}
	// The oldest entry of a ref has the highest selector number.
	oldest := map[string]int{}
	subject := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		sel, msg, ok := strings.Cut(strings.TrimRight(line, "\r"), "\x00")
		if !ok {
			continue
		}
		at := strings.LastIndex(sel, "@{")
		name, found := strings.CutPrefix(sel[:max(at, 0)], "refs/heads/")
		if at < 0 || !found || !strings.HasSuffix(sel, "}") {
			continue
		}
		n, err := strconv.Atoi(sel[at+2 : len(sel)-1])
		if err != nil {
			continue
		}
		if prev, seen := oldest[name]; !seen || n > prev {
			oldest[name], subject[name] = n, strings.TrimSpace(msg)
		}
	}
	res := map[string]string{}
	for name, msg := range subject {
		if start, ok := strings.CutPrefix(msg, createdFrom); ok {
			res[name] = strings.TrimSpace(start)
		}
	}
	return res
}

// trunkNamed returns the tied trunk that start names: the trunk's name, its
// refs/heads/ form, or either spelling on a remote of the snapshot.
func trunkNamed(start string, tied, remotes []string) string {
	if start == "" {
		return ""
	}
	for _, t := range tied {
		if start == t || start == "refs/heads/"+t {
			return t
		}
		for _, rm := range remotes {
			if start == rm+"/"+t || start == "refs/remotes/"+rm+"/"+t {
				return t
			}
		}
	}
	return ""
}

// remoteNames lists the remotes that have a tracking ref in the snapshot.
func remoteNames(snap repo.Snapshot) []string {
	seen := map[string]bool{}
	var out []string
	for _, ref := range snap.Remote {
		if i := strings.Index(ref.Name, "/"); i > 0 && !seen[ref.Name[:i]] {
			seen[ref.Name[:i]] = true
			out = append(out, ref.Name[:i])
		}
	}
	return out
}
