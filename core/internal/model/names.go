package model

import "strings"

// ShortName removes the configured personal prefix from a branch name.
func ShortName(branch, hidePrefix string) string {
	if hidePrefix != "" && strings.HasPrefix(branch, hidePrefix) {
		return branch[len(hidePrefix):]
	}
	return branch
}

// Topic is the part of the branch name after the last slash.
func Topic(branch string) string {
	return branch[strings.LastIndex(branch, "/")+1:]
}
