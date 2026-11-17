package gitx

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Version is a git major.minor version.
type Version struct{ Major, Minor int }

// AtLeast reports whether v >= major.minor.
func (v Version) AtLeast(major, minor int) bool {
	if v.Major != major {
		return v.Major > major
	}
	return v.Minor >= minor
}

// DetectVersion runs `git version` through r and parses the result.
func DetectVersion(ctx context.Context, r Runner) (Version, error) {
	out, err := r.Run(ctx, "", "version")
	if err != nil {
		return Version{}, err
	}
	return parseVersion(string(out))
}

func parseVersion(s string) (Version, error) {
	f := strings.Fields(s)
	if len(f) < 3 || f[0] != "git" || f[1] != "version" {
		return Version{}, fmt.Errorf("unrecognized git version output %q", s)
	}
	parts := strings.Split(f[2], ".")
	if len(parts) < 2 {
		return Version{}, fmt.Errorf("unrecognized git version %q", f[2])
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return Version{}, fmt.Errorf("unrecognized git version %q", f[2])
	}
	return Version{major, minor}, nil
}
