package rpc

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/settings"
)

// countRunner counts the git commands run through it.
type countRunner struct {
	gitx.ExecRunner
	mu sync.Mutex
	n  int
}

func (c *countRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return c.ExecRunner.Run(ctx, dir, args...)
}

func (c *countRunner) RunInput(ctx context.Context, dir string, input []byte, args ...string) ([]byte, error) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return c.ExecRunner.RunInput(ctx, dir, input, args...)
}

// The methods that take no read slot are exactly these, and they run no git.
func TestNoGitMethodsArePinned(t *testing.T) {
	run := &countRunner{}
	h, err := NewHandler(run, settings.Store{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"version": true, "repos": true, "getGlobal": true, "backups": true, "cancel": true}
	for name, m := range h.methods {
		if m.noGit != want[name] {
			t.Errorf("method %s: noGit=%v, want %v", name, m.noGit, want[name])
		}
	}
	run.n = 0
	for name := range want {
		var params json.RawMessage
		if name == "cancel" {
			params = json.RawMessage(`{"target":1}`)
		}
		if resp := h.Handle(context.Background(), Request{ID: 1, Method: name, Params: params}); resp.Error != nil {
			t.Fatalf("%s: %+v", name, resp.Error)
		}
	}
	if run.n != 0 {
		t.Fatalf("the no-git methods ran %d git commands", run.n)
	}
}

// With maxQueuedReads reads waiting for a slot, one more read is answered at
// once; the waiting ones can still be cancelled.
func TestTooManyQueuedReads(t *testing.T) {
	c, run, r := heldServer(t)
	var lines []string
	for id := 100; id < 100+maxQueuedReads; id++ {
		lines = append(lines, discoverReq(id, r.Dir))
	}
	lines = append(lines, discoverReq(5000, r.Dir))
	c.sendLater(lines...)
	rep, ok := c.readReplyWithin(10 * time.Second)
	if !ok || rep.ID != 5000 || rep.Error == nil || rep.Error.Code != "internal" {
		t.Fatalf("the read past the queue answered %+v (ok=%v)", rep, ok)
	}
	lines = lines[:0]
	for id := 100; id < 100+maxQueuedReads; id++ {
		lines = append(lines, `{"id":`+strconv.Itoa(10000+id)+`,"method":"cancel","params":{"target":`+strconv.Itoa(id)+`}}`)
	}
	c.sendLater(lines...)
	for id, rep := range c.replies(2*maxQueuedReads, 20*time.Second) {
		if id < 10000 && (rep.Error == nil || rep.Error.Code != "cancelled") {
			t.Fatalf("waiting read %d answered %+v", id, rep)
		}
	}
	run.free()
	c.replies(maxConcurrent, 20*time.Second)
}
