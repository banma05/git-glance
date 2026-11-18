package rpc

import (
	"context"
	"io"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/testrepo"
)

// heldRunner holds every `worktree` call (discover, addRepo and the other
// methods that find a repository) until it is freed or the call's context
// ends, and reports each start.
type heldRunner struct {
	gitx.Runner
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newHeldRunner() *heldRunner {
	return &heldRunner{Runner: gitx.ExecRunner{}, started: make(chan struct{}, 256), release: make(chan struct{})}
}

func (h *heldRunner) free() { h.once.Do(func() { close(h.release) }) }

func (h *heldRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if len(args) > 0 && args[0] == "worktree" {
		select {
		case h.started <- struct{}{}:
		default:
		}
		select {
		case <-h.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return h.Runner.Run(ctx, dir, args...)
}

func (h *heldRunner) waitStarted(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-h.started:
		case <-time.After(10 * time.Second):
			t.Fatalf("%d of %d held git calls started", i, n)
		}
	}
}

func discoverReq(id int, dir string) string {
	return `{"id":` + strconv.Itoa(id) + `,"method":"discover","params":{"path":` + strconv.Quote(dir) + `}}`
}

// heldServer starts ServeLines with every read slot taken by a discover held
// in git (ids 1 to maxConcurrent).
func heldServer(t *testing.T) (*client, *heldRunner, *testrepo.Repo) {
	t.Helper()
	r := testrepo.New(t)
	run := newHeldRunner()
	c := startServerWith(t, r, run)
	t.Cleanup(run.free) // runs before the server's cleanup
	for id := 1; id <= maxConcurrent; id++ {
		c.sendRaw(discoverReq(id, r.Dir))
	}
	run.waitStarted(t, maxConcurrent)
	return c, run, r
}

// sendLater sends lines from another goroutine: a server that stops reading
// lines must fail the test, not hang it.
func (c *client) sendLater(lines ...string) {
	go func() {
		for _, l := range lines {
			if _, err := io.WriteString(c.w, l+"\n"); err != nil {
				return
			}
		}
	}()
}

// replies reads n replies, by id.
func (c *client) replies(n int, within time.Duration) map[int64]reply {
	c.t.Helper()
	got := map[int64]reply{}
	for len(got) < n {
		rep, ok := c.readReplyWithin(within)
		if !ok {
			c.t.Fatalf("only %d of %d replies came (ids %v)", len(got), n, keys(got))
		}
		got[rep.ID] = rep
	}
	return got
}

func keys(m map[int64]reply) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// With every read slot taken and another read waiting for one, a method that
// runs no git and a write are still read and answered.
func TestFullReadSlotsDoNotBlockOtherRequests(t *testing.T) {
	c, run, r := heldServer(t)
	c.sendLater(discoverReq(17, r.Dir), // waits for a slot
		`{"id":18,"method":"repos"}`,
		`{"id":19,"method":"setGlobal","params":{"theme":"dark"}}`)
	got := c.replies(2, 5*time.Second)
	for _, id := range []int64{18, 19} {
		if rep, ok := got[id]; !ok || rep.Error != nil {
			t.Fatalf("reply %d = %+v (got %v)", id, rep, keys(got))
		}
	}
	run.free()
	for id, rep := range c.replies(maxConcurrent+1, 20*time.Second) {
		if rep.Error != nil {
			t.Fatalf("discover %d: %+v", id, rep.Error)
		}
	}
}

// cancel stops a running read, which answers cancelled and frees its slot
// for a waiting read, and stops a read still waiting for a slot.
func TestCancelStopsReads(t *testing.T) {
	c, run, r := heldServer(t)
	c.sendLater(discoverReq(17, r.Dir), discoverReq(18, r.Dir), // both wait for a slot
		`{"id":19,"method":"cancel","params":{"target":18}}`)
	got := c.replies(2, 5*time.Second)
	if rep := got[19]; rep.Error != nil || string(rep.Result) != "{}" {
		t.Fatalf("cancel answered %+v (got %v)", rep, keys(got))
	}
	if rep := got[18]; rep.Error == nil || rep.Error.Code != "cancelled" {
		t.Fatalf("the waiting read answered %+v", rep)
	}
	c.sendLater(`{"id":20,"method":"cancel","params":{"target":3}}`)
	got = c.replies(2, 5*time.Second)
	if rep := got[20]; rep.Error != nil || string(rep.Result) != "{}" {
		t.Fatalf("cancel answered %+v (got %v)", rep, keys(got))
	}
	if rep := got[3]; rep.Error == nil || rep.Error.Code != "cancelled" {
		t.Fatalf("the running read answered %+v", rep)
	}
	run.waitStarted(t, 1) // read 17 got the freed slot
	run.free()
	got = c.replies(maxConcurrent, 20*time.Second)
	for id, rep := range got {
		if rep.Error != nil || id == 3 || id == 18 {
			t.Fatalf("reply %d: %+v", id, rep)
		}
	}
}

// A write is never cancelled: cancel answers {} and the write finishes.
func TestCancelDoesNotStopAWrite(t *testing.T) {
	r := testrepo.New(t)
	run := newHeldRunner()
	c := startServerWith(t, r, run)
	t.Cleanup(run.free)
	c.sendRaw(`{"id":1,"method":"addRepo","params":{"path":` + strconv.Quote(r.Dir) + `}}`)
	run.waitStarted(t, 1)
	c.sendRaw(`{"id":2,"method":"cancel","params":{"target":1}}`)
	rep, ok := c.readReplyWithin(5 * time.Second)
	if !ok || rep.ID != 2 || rep.Error != nil || string(rep.Result) != "{}" {
		t.Fatalf("cancel answered %+v (ok=%v)", rep, ok)
	}
	time.Sleep(200 * time.Millisecond) // time for a wrong cancel to land
	run.free()
	rep, ok = c.readReplyWithin(10 * time.Second)
	if !ok || rep.ID != 1 || rep.Error != nil {
		t.Fatalf("the write answered %+v (ok=%v)", rep, ok)
	}
	var repos []string
	c.ok("repos", nil, &repos)
	if len(repos) != 1 {
		t.Fatalf("repos = %v", repos)
	}
}

// cancel of a finished or unknown request answers {}; its target is
// required.
func TestCancelUnknownTarget(t *testing.T) {
	c := startServer(t, nil)
	c.ok("version", nil, nil)
	for _, target := range []int64{1, 99} {
		rep := c.call("cancel", map[string]any{"target": target})
		if rep.Error != nil || string(rep.Result) != "{}" {
			t.Fatalf("cancel %d answered %+v", target, rep)
		}
	}
	for _, params := range []any{nil, map[string]any{}, map[string]any{"target": "x"}} {
		if code := c.errCode("cancel", params); code != "bad-params" {
			t.Fatalf("cancel %v: code %s", params, code)
		}
	}
}
