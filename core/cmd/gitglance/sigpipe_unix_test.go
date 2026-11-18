//go:build !windows

package main

import (
	"os/signal"
	"syscall"
	"testing"
)

// An ignored SIGPIPE is inherited by every child: git and the user's hooks
// would then run with it ignored. The core catches it instead, which a child
// does not inherit, and a write to a closed stdout still returns EPIPE.
func TestSigpipeIsCaughtNotIgnored(t *testing.T) {
	if signal.Ignored(syscall.SIGPIPE) {
		t.Fatal("SIGPIPE is ignored: git and hooks would inherit SIG_IGN")
	}
}
