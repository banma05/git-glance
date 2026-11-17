// Package rpc is the external surface of the core: a JSON-lines protocol
// (one request per line, one response per line) over any reader and writer,
// and the Handler that the browser mode serves over HTTP as well.
// Every parameter is untrusted input.
package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/repo"
	"github.com/banma05/git-glance/core/internal/settings"
)

const (
	maxLine         = 1 << 20 // longest accepted request line
	maxConcurrent   = 16      // read requests handled at the same time
	maxQueuedWrites = 1024    // writes waiting for the write worker
)

// Request is one call. Params is decoded by the method.
type Request struct {
	ID     int64           `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response answers the request with the same ID. Exactly one of Result and
// Error is set.
type Response struct {
	ID     int64     `json:"id"`
	Result any       `json:"result,omitempty"`
	Error  *RPCError `json:"error,omitempty"`
}

// RPCError codes: not-repo, bad-params, unknown-method, git-failed,
// not-allowed, editor-not-found, internal.
type RPCError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return e.Code + ": " + e.Message }

func badParams(format string, args ...any) error {
	return &RPCError{Code: "bad-params", Message: fmt.Sprintf(format, args...)}
}

// toRPCError maps any error to the protocol's error.
func toRPCError(err error) *RPCError {
	var re *RPCError
	var ge *gitx.GitError
	switch {
	case errors.As(err, &re):
		return re
	case errors.Is(err, gitx.ErrNotAllowed):
		return &RPCError{Code: "not-allowed", Message: err.Error()}
	case errors.As(err, &ge):
		return &RPCError{Code: "git-failed", Message: lastStderrLine(ge.Stderr)}
	case errors.Is(err, repo.ErrNotRepo):
		return &RPCError{Code: "not-repo", Message: err.Error()}
	}
	return &RPCError{Code: "internal", Message: err.Error()}
}

func lastStderrLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\r\n \t"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return "git failed"
}

type fetchState struct {
	at  int64
	err string
}

// Handler runs the methods. It is safe for concurrent use: read-only methods
// run in parallel, methods that change the local machine queue on one lock.
type Handler struct {
	r           gitx.Runner
	st          settings.Store
	coreVersion string
	gitVersion  gitx.Version
	gitFound    bool
	now         func() time.Time

	write sync.Mutex // held by every method that changes the local machine

	fetchMu sync.Mutex
	fetched map[string]fetchState // by repo root

	lookPath    func(name string) (string, error)
	startEditor func(exe, path string) error
	methods     map[string]method
}

// NewHandler detects the git version once. A missing or unusable git is not
// an error: version reports gitOK=false and git-using methods fail later.
func NewHandler(r gitx.Runner, st settings.Store) (*Handler, error) {
	if st.Dir == "" {
		return nil, errors.New("settings store has no directory")
	}
	h := &Handler{
		r: r, st: st, coreVersion: "dev", now: time.Now,
		fetched:     map[string]fetchState{},
		lookPath:    lookPathOutsideCwd,
		startEditor: launchEditor,
	}
	if v, err := gitx.DetectVersion(context.Background(), r); err == nil {
		h.gitVersion, h.gitFound = v, true
	}
	h.methods = h.table()
	return h, nil
}

func (h *Handler) isWrite(name string) bool { return h.methods[name].write }

// SetCoreVersion sets the version string reported by the version method.
func (h *Handler) SetCoreVersion(v string) { h.coreVersion = v }

// Handle runs one request. It never panics.
func (h *Handler) Handle(ctx context.Context, req Request) (resp Response) {
	resp.ID = req.ID
	defer func() {
		if p := recover(); p != nil {
			resp = Response{ID: req.ID, Error: &RPCError{Code: "internal", Message: fmt.Sprint("panic: ", p)}}
		}
	}()
	m, ok := h.methods[req.Method]
	if !ok {
		resp.Error = &RPCError{Code: "unknown-method", Message: "unknown method " + req.Method}
		return resp
	}
	if m.write {
		h.write.Lock()
		defer h.write.Unlock()
	}
	res, err := m.fn(ctx, req.Params)
	if err != nil {
		resp.Error = toRPCError(err)
		return resp
	}
	resp.Result = res
	return resp
}

// ServeLines reads one JSON request per line from in and writes one JSON
// response per line to out. Requests run concurrently, so responses may come
// out of order. It stops reading at EOF or when ctx is done, then waits for
// the running requests before returning. A line longer than 1 MiB gets a
// bad-params response with id 0 and the server keeps going.
func ServeLines(ctx context.Context, h *Handler, in io.Reader, out io.Writer) error {
	lines := make(chan lineItem)
	stop := make(chan struct{})
	defer close(stop)
	go readLines(in, lines, stop)

	var (
		outMu sync.Mutex
		wg    sync.WaitGroup
		sem   = make(chan struct{}, maxConcurrent)
	)
	send := func(resp Response) {
		b, err := json.Marshal(resp)
		if err != nil {
			b, _ = json.Marshal(Response{ID: resp.ID, Error: &RPCError{Code: "internal", Message: "encoding the result failed"}})
		}
		outMu.Lock()
		defer outMu.Unlock()
		out.Write(append(b, '\n'))
	}
	// Writes run one at a time in arrival order on their own worker, which
	// holds no read slot, so queued writes never stall reads.
	writes := make(chan Request, maxQueuedWrites)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for req := range writes {
			if ctx.Err() != nil {
				send(Response{ID: req.ID, Error: &RPCError{Code: "internal", Message: "stopped before the request started"}})
				continue
			}
			send(h.Handle(ctx, req))
		}
	}()
	defer func() {
		close(writes)
		wg.Wait()
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case it, open := <-lines:
			if !open {
				return nil
			}
			if it.err != nil {
				return it.err
			}
			if it.tooLong {
				send(Response{Error: &RPCError{Code: "bad-params", Message: "request line is longer than 1 MiB"}})
				continue
			}
			if len(strings.TrimSpace(it.text)) == 0 {
				continue
			}
			var req Request
			if err := json.Unmarshal([]byte(it.text), &req); err != nil {
				send(Response{Error: &RPCError{Code: "bad-params", Message: "request is not valid JSON"}})
				continue
			}
			if h.isWrite(req.Method) {
				select {
				case writes <- req:
				default:
					send(Response{ID: req.ID, Error: &RPCError{Code: "internal", Message: "too many queued writes"}})
				}
				continue
			}
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return ctx.Err()
			}
			wg.Add(1)
			go func() {
				defer func() { <-sem; wg.Done() }()
				send(h.Handle(ctx, req))
			}()
		}
	}
}

type lineItem struct {
	text    string
	tooLong bool
	err     error
}

// readLines splits in into lines of at most maxLine bytes. A longer line is
// skipped to its end and reported once. It exits when in ends or stop closes
// (it may stay blocked in a Read until the input closes).
func readLines(in io.Reader, out chan<- lineItem, stop <-chan struct{}) {
	defer close(out)
	br := bufio.NewReaderSize(in, 64*1024)
	emit := func(it lineItem) bool {
		select {
		case out <- it:
			return true
		case <-stop:
			return false
		}
	}
	var buf []byte
	tooLong := false
	for {
		chunk, isPrefix, err := br.ReadLine()
		if err != nil {
			if err != io.EOF {
				emit(lineItem{err: err})
			}
			return
		}
		if !tooLong {
			buf = append(buf, chunk...)
			if len(buf) > maxLine {
				tooLong, buf = true, nil
			}
		}
		if isPrefix {
			continue
		}
		it := lineItem{text: string(buf), tooLong: tooLong}
		buf, tooLong = nil, false
		if !emit(it) {
			return
		}
	}
}
