//go:build !windows

package gitx

import "os/exec"

func hideConsole(*exec.Cmd) {}
