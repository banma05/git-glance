//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// A write to a closed stdout (the editor died) must fail with EPIPE instead of
// killing the core in the middle of a write. Catching SIGPIPE does that;
// ignoring it would also pass SIG_IGN on to git and the user's hooks.
func init() { signal.Notify(make(chan os.Signal, 1), syscall.SIGPIPE) }
