package main

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestFirstSignalPrintsHintAndReleasesSignals(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var out syncBuf
	stopped := make(chan struct{})
	finished := make(chan struct{})
	go watchSignals(ctx, func() { close(stopped) }, finished, &out)
	cancel() // the first Ctrl-C
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("stop was not called after the first signal, so a second Ctrl-C would be swallowed")
	}
	if !strings.Contains(out.String(), "Ctrl-C again to force quit") {
		t.Fatalf("hint = %q", out.String())
	}
}

func TestNormalExitPrintsNoHint(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var out syncBuf
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() { watchSignals(ctx, func() {}, finished, &out); close(done) }()
	close(finished) // run returned on its own
	cancel()        // main's stop() cancels ctx afterwards
	<-done
	if out.String() != "" {
		t.Fatalf("unexpected output %q", out.String())
	}
}
