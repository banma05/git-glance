//go:build !windows

package main

import (
	"os/signal"
	"syscall"
)

// A write to a closed stdout (the editor died) must fail with EPIPE instead of
// killing the core in the middle of a write.
func init() { signal.Ignore(syscall.SIGPIPE) }
