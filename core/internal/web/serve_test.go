package web

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/banma05/git-glance/core/internal/gitx"
	"github.com/banma05/git-glance/core/internal/rpc"
	"github.com/banma05/git-glance/core/internal/settings"
	"github.com/banma05/git-glance/core/internal/testrepo"
)

const testToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func newHandlerAt(t *testing.T, r gitx.Runner, dir string) *rpc.Handler {
	t.Helper()
	h, err := rpc.NewHandler(r, settings.Store{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func newHandler(t *testing.T, r gitx.Runner) *rpc.Handler {
	t.Helper()
	return newHandlerAt(t, r, t.TempDir())
}

func startWeb(t *testing.T, h *rpc.Handler) *httptest.Server {
	t.Helper()
	ui := fstest.MapFS{
		"index.html": {Data: []byte("<html>index</html>")},
		"app.js":     {Data: []byte("console.log(1)")},
	}
	ts := httptest.NewUnstartedServer(NewServer(h, ui, testToken))
	hardenServer(ts.Config)
	ts.Start()
	t.Cleanup(ts.Close)
	return ts
}

func plain(t *testing.T) *httptest.Server {
	return startWeb(t, newHandler(t, gitx.ExecRunner{}))
}

func portOf(ts *httptest.Server) string {
	addr := ts.Listener.Addr().String()
	return addr[strings.LastIndex(addr, ":")+1:]
}

type opt func(*http.Request)

func withToken(tok string) opt {
	return func(r *http.Request) { r.Header.Set("X-GitGlance-Token", tok) }
}
func withHeader(k, v string) opt { return func(r *http.Request) { r.Header.Set(k, v) } }
func withHost(h string) opt      { return func(r *http.Request) { r.Host = h } }

func do(t *testing.T, ts *httptest.Server, method, path, body string, opts ...opt) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range opts {
		o(req)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// rpcPost sends a well-formed rpc call; opts may override any header.
func rpcPost(t *testing.T, ts *httptest.Server, body string, opts ...opt) *http.Response {
	t.Helper()
	all := append([]opt{withToken(testToken), withHeader("Content-Type", "application/json")}, opts...)
	return do(t, ts, "POST", "/rpc", body, all...)
}

// rawExchange sends raw bytes and reads one response.
func rawExchange(t *testing.T, addr, raw string) *http.Response {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	io.WriteString(conn, raw)
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

const versionReq = `{"id":1,"method":"version"}`

func TestRPCNeedsToken(t *testing.T) {
	ts := plain(t)
	ct := withHeader("Content-Type", "application/json")
	if c := do(t, ts, "POST", "/rpc", versionReq, ct).StatusCode; c != 403 {
		t.Fatalf("POST /rpc without token = %d", c)
	}
	if c := do(t, ts, "POST", "/rpc", versionReq, ct, withToken("")).StatusCode; c != 403 {
		t.Fatalf("POST /rpc with empty token = %d", c)
	}
}

func TestTokenFlow(t *testing.T) {
	ts := plain(t)
	resp := do(t, ts, "GET", "/", "")
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(b), "index") {
		t.Fatalf("GET / = %d %q", resp.StatusCode, b)
	}
	r := rpcPost(t, ts, versionReq)
	rb, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 || !strings.Contains(string(rb), `"gitOK":true`) {
		t.Fatalf("rpc = %d %s", r.StatusCode, rb)
	}
	r2 := rpcPost(t, ts, versionReq, withHeader("Content-Type", "application/json; charset=utf-8"))
	if r2.StatusCode != 200 {
		t.Fatalf("json with charset = %d", r2.StatusCode)
	}
}

func TestStaticNeedsNoTokenButAllowedHost(t *testing.T) {
	ts := plain(t)
	if c := do(t, ts, "GET", "/app.js", "").StatusCode; c != 200 {
		t.Fatalf("static without token = %d", c)
	}
	if c := do(t, ts, "HEAD", "/app.js", "").StatusCode; c != 200 {
		t.Fatalf("HEAD = %d", c)
	}
	if c := do(t, ts, "GET", "/app.js", "", withHost("evil.example:1")).StatusCode; c != 403 {
		t.Fatalf("static with bad host = %d", c)
	}
	if c := do(t, ts, "POST", "/app.js", "x").StatusCode; c != 405 {
		t.Fatalf("POST static = %d", c)
	}
	if c := do(t, ts, "GET", "/missing.js", "").StatusCode; c != 404 {
		t.Fatalf("missing = %d", c)
	}
	// ?token= is not special any more: no cookie, same answer.
	r := do(t, ts, "GET", "/?token="+testToken, "")
	if r.StatusCode != 200 || len(r.Header.Values("Set-Cookie")) != 0 {
		t.Fatalf("query token: %d, cookies %v", r.StatusCode, r.Header.Values("Set-Cookie"))
	}
}

func TestBadHost(t *testing.T) {
	ts := plain(t)
	port := portOf(ts)
	for _, h := range []string{"evil.example:1234", "evil.example", "127.0.0.1", "127.0.0.1:1",
		"LOCALHOST:" + port, "localhost.:" + port, "[::1]:" + port, "127.0.0.1:" + port + ".evil.example"} {
		if c := do(t, ts, "GET", "/", "", withHost(h)).StatusCode; c != 403 {
			t.Errorf("static, host %q = %d", h, c)
		}
		if c := rpcPost(t, ts, versionReq, withHost(h)).StatusCode; c != 403 {
			t.Errorf("rpc, host %q = %d", h, c)
		}
	}
	for _, h := range []string{"localhost:" + port, "127.0.0.1:" + port} {
		if c := do(t, ts, "GET", "/", "", withHost(h)).StatusCode; c != 200 {
			t.Errorf("host %q = %d", h, c)
		}
	}
}

func TestOriginRules(t *testing.T) {
	ts := plain(t)
	port := portOf(ts)
	for _, o := range []string{"http://evil.example", "null", "http://127.0.0.1:1", "https://127.0.0.1:" + port,
		"http://LOCALHOST:" + port, "http://[::1]:" + port} {
		if c := rpcPost(t, ts, versionReq, withHeader("Origin", o)).StatusCode; c != 403 {
			t.Errorf("origin %q = %d", o, c)
		}
	}
	for _, o := range []string{"http://127.0.0.1:" + port, "http://localhost:" + port} {
		if c := rpcPost(t, ts, versionReq, withHeader("Origin", o)).StatusCode; c != 200 {
			t.Errorf("origin %q = %d", o, c)
		}
	}
}

func TestContentTypeRequired(t *testing.T) {
	ts := plain(t)
	for _, ct := range []string{"text/plain", "application/x-www-form-urlencoded", "multipart/form-data", ""} {
		resp := do(t, ts, "POST", "/rpc", versionReq, withToken(testToken), withHeader("Content-Type", ct))
		if resp.StatusCode != 415 {
			t.Errorf("content type %q = %d", ct, resp.StatusCode)
		}
	}
}

func TestWrongTokens(t *testing.T) {
	ts := plain(t)
	off := testToken[:len(testToken)-1] + "0"
	for _, tok := range []string{"nope", off, testToken + "0", testToken[:10]} {
		if c := rpcPost(t, ts, versionReq, withToken(tok)).StatusCode; c != 403 {
			t.Errorf("token %q = %d", tok, c)
		}
	}
	// A cookie or a query parameter never works as a token.
	r := do(t, ts, "POST", "/rpc?token="+testToken, versionReq, withHeader("Content-Type", "application/json"),
		withHeader("Cookie", "gg_token="+testToken))
	if r.StatusCode != 403 {
		t.Fatalf("cookie+query token = %d", r.StatusCode)
	}
}

func TestMethodsOnRPC(t *testing.T) {
	ts := plain(t)
	for _, m := range []string{"GET", "PUT", "DELETE", "OPTIONS"} {
		if c := do(t, ts, m, "/rpc", "", withToken(testToken)).StatusCode; c != 405 {
			t.Fatalf("%s /rpc = %d", m, c)
		}
	}
}

func TestSecurityHeadersNoCORSNoCookies(t *testing.T) {
	ts := plain(t)
	resps := []*http.Response{
		do(t, ts, "GET", "/", "", withHost("evil.example:1")),
		do(t, ts, "GET", "/", ""),
		do(t, ts, "GET", "/?token="+testToken, ""),
		rpcPost(t, ts, versionReq),
		rpcPost(t, ts, versionReq, withToken("x")),
		do(t, ts, "GET", "/rpc", ""),
		do(t, ts, "OPTIONS", "/rpc", "", withHeader("Origin", "http://evil")),
		do(t, ts, "GET", "/nothing", ""),
		rawExchange(t, ts.Listener.Addr().String(), "OPTIONS * HTTP/1.1\r\nHost: "+ts.Listener.Addr().String()+"\r\n\r\n"),
	}
	want := map[string]string{
		"Referrer-Policy":         "no-referrer",
		"Cache-Control":           "no-store",
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "frame-ancestors 'none'",
	}
	for i, r := range resps {
		for k, v := range want {
			if got := r.Header.Get(k); got != v {
				t.Errorf("response %d (%d): %s = %q", i, r.StatusCode, k, got)
			}
		}
		for k := range r.Header {
			if strings.HasPrefix(strings.ToLower(k), "access-control-") {
				t.Errorf("response %d has CORS header %s", i, k)
			}
		}
		if len(r.Header.Values("Set-Cookie")) != 0 {
			t.Errorf("response %d sets a cookie", i)
		}
	}
	if resps[0].StatusCode != 403 || resps[1].StatusCode != 200 {
		t.Fatalf("unexpected statuses %d %d", resps[0].StatusCode, resps[1].StatusCode)
	}
}

func TestOptionsStarIsNotAnswered200(t *testing.T) {
	ts := plain(t)
	good := rawExchange(t, ts.Listener.Addr().String(), "OPTIONS * HTTP/1.1\r\nHost: "+ts.Listener.Addr().String()+"\r\n\r\n")
	if good.StatusCode == 200 {
		t.Fatalf("OPTIONS * = 200")
	}
	bad := rawExchange(t, ts.Listener.Addr().String(), "OPTIONS * HTTP/1.1\r\nHost: evil.example\r\n\r\n")
	if bad.StatusCode != 403 {
		t.Fatalf("OPTIONS * with bad host = %d", bad.StatusCode)
	}
}

func TestRefusalsCloseTheConnection(t *testing.T) {
	ts := plain(t)
	big := `{"id":1,"method":"version","params":"` + strings.Repeat("a", 1<<20) + `"}`
	cases := map[string]*http.Response{
		"403 host":  do(t, ts, "GET", "/", "", withHost("evil.example:1")),
		"403 token": rpcPost(t, ts, versionReq, withToken("x")),
		"405 rpc":   do(t, ts, "GET", "/rpc", ""),
		"405 file":  do(t, ts, "POST", "/app.js", "x"),
		"413":       rpcPost(t, ts, big),
		"415":       rpcPost(t, ts, versionReq, withHeader("Content-Type", "text/plain")),
	}
	for name, r := range cases {
		if !r.Close {
			t.Errorf("%s did not send Connection: close", name)
		}
	}
}

func TestNewServerRefusesEmptyToken(t *testing.T) {
	defer func() {
		p := recover()
		if p == nil || !strings.Contains(p.(string), "empty token") {
			t.Fatalf("recovered %v", p)
		}
	}()
	NewServer(newHandler(t, gitx.ExecRunner{}), fstest.MapFS{}, "")
}

func TestTokenNeverEchoed(t *testing.T) {
	ts := plain(t)
	for _, r := range []*http.Response{
		rpcPost(t, ts, versionReq, withToken(testToken[:10])),
		rpcPost(t, ts, versionReq, withHeader("Origin", "http://evil.example")),
		rpcPost(t, ts, `{"bad`),
	} {
		b, _ := io.ReadAll(r.Body)
		if strings.Contains(string(b), testToken[:10]) || strings.Contains(strings.Join(r.Header.Values("Set-Cookie"), ""), testToken[:10]) {
			t.Fatalf("token echoed in %d %q", r.StatusCode, b)
		}
	}
}

func TestOversizeBodyRefused(t *testing.T) {
	ts := plain(t)
	big := `{"id":1,"method":"version","params":"` + strings.Repeat("a", 1<<20) + `"}`
	if c := rpcPost(t, ts, big).StatusCode; c != 413 {
		t.Fatalf("oversize = %d", c)
	}
	if c := rpcPost(t, ts, versionReq).StatusCode; c != 200 {
		t.Fatalf("small = %d", c)
	}
}

func TestBadJSONGivesBadParams(t *testing.T) {
	ts := plain(t)
	r := rpcPost(t, ts, `{"bad`)
	b, _ := io.ReadAll(r.Body)
	if r.StatusCode != 400 || !strings.Contains(string(b), "bad-params") {
		t.Fatalf("bad json = %d %s", r.StatusCode, b)
	}
}

// gateRunner blocks every `worktree list` made in a "gate-" folder (addRepo makes
// one) until released, and reports which folder reached git. A run that was
// handed a done context reports it on ctxErr.
type gateRunner struct {
	gitx.Runner
	started chan string
	release chan struct{}
	ctxErr  chan error
}

func (g *gateRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if len(args) > 0 && args[0] == "worktree" && strings.Contains(dir, "gate-") {
		g.started <- dir
		<-g.release
		if g.ctxErr != nil {
			g.ctxErr <- ctx.Err()
		}
	}
	return g.Runner.Run(ctx, dir, args...)
}

func addRepoReq(id int, dir string) string {
	return `{"id":` + strconv.Itoa(id) + `,"method":"addRepo","params":{"path":` + strconv.Quote(dir) + `}}`
}

func gatedRunner() *gateRunner {
	return &gateRunner{Runner: gitx.ExecRunner{}, started: make(chan string, 8), release: make(chan struct{}, 8), ctxErr: make(chan error, 8)}
}

func gateDirs(t *testing.T) []string {
	t.Helper()
	tr := testrepo.New(t)
	tr.ApplyEnv()
	var dirs []string
	for _, n := range []string{"gate-a", "gate-b", "gate-c"} {
		dirs = append(dirs, tr.Clone(n))
	}
	return dirs
}

func waitStart(t *testing.T, g *gateRunner) string {
	t.Helper()
	select {
	case d := <-g.started:
		return d
	case <-time.After(5 * time.Second):
		t.Fatal("write did not start")
		return ""
	}
}

func TestHTTPWritesRunInArrivalOrder(t *testing.T) {
	dirs := gateDirs(t)
	g := gatedRunner()
	ts := startWeb(t, newHandler(t, g))
	var wg sync.WaitGroup
	post := func(i int) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rpcPost(t, ts, addRepoReq(i+1, dirs[i]))
		}()
	}
	post(0)
	if d := waitStart(t, g); d != dirs[0] {
		t.Fatalf("first write on %s", d)
	}
	time.Sleep(100 * time.Millisecond)
	post(1)
	time.Sleep(100 * time.Millisecond)
	post(2)
	time.Sleep(100 * time.Millisecond)
	select {
	case d := <-g.started:
		t.Fatalf("second write started on %s while the first was running", d)
	default:
	}
	g.release <- struct{}{}
	if d := waitStart(t, g); d != dirs[1] {
		t.Fatalf("second write on %s", d)
	}
	g.release <- struct{}{}
	if d := waitStart(t, g); d != dirs[2] {
		t.Fatalf("third write on %s", d)
	}
	g.release <- struct{}{}
	wg.Wait()
}

type lockedWriter struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func TestHTTPAndLineWritesShareOneQueue(t *testing.T) {
	dirs := gateDirs(t)
	g := gatedRunner()
	h := newHandler(t, g)
	ts := startWeb(t, h)
	inR, inW := io.Pipe()
	var out lockedWriter
	done := make(chan error, 1)
	go func() { done <- rpc.ServeLines(context.Background(), h, inR, &out) }()
	io.WriteString(inW, addRepoReq(1, dirs[0])+"\n")
	if d := waitStart(t, g); d != dirs[0] {
		t.Fatalf("line write on %s", d)
	}
	httpDone := make(chan int, 1)
	go func() { httpDone <- rpcPost(t, ts, addRepoReq(2, dirs[1])).StatusCode }()
	time.Sleep(150 * time.Millisecond)
	select {
	case d := <-g.started:
		t.Fatalf("HTTP write started on %s while a ServeLines write was running", d)
	default:
	}
	g.release <- struct{}{}
	if d := waitStart(t, g); d != dirs[1] {
		t.Fatalf("http write on %s", d)
	}
	g.release <- struct{}{}
	if c := <-httpDone; c != 200 {
		t.Fatalf("http write = %d", c)
	}
	inW.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// A write that has started is finished even when the client leaves: the
// repo is added and git's context was never cancelled.
func TestWriteSurvivesClientDisconnect(t *testing.T) {
	dirs := gateDirs(t)
	g := gatedRunner()
	store := t.TempDir()
	ts := startWeb(t, newHandlerAt(t, g, store))

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "POST", ts.URL+"/rpc", strings.NewReader(addRepoReq(1, dirs[0])))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitGlance-Token", testToken)
	clientDone := make(chan error, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
		}
		clientDone <- err
	}()
	waitStart(t, g)
	cancel()
	if err := <-clientDone; err == nil {
		t.Fatal("client was not cancelled")
	}
	time.Sleep(200 * time.Millisecond) // let the server notice the disconnect
	g.release <- struct{}{}
	select {
	case err := <-g.ctxErr:
		if err != nil {
			t.Fatalf("git ran with a cancelled context: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("write never reached the end of git")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		gl, err := settings.Store{Dir: store}.Global()
		if err == nil && len(gl.Repos) == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("repo was not added: %+v %v", gl, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A read stops when its HTTP client goes away: git sees a cancelled context.
func TestReadStopsWhenTheClientGoesAway(t *testing.T) {
	dirs := gateDirs(t)
	g := gatedRunner()
	ts := startWeb(t, newHandlerAt(t, g, t.TempDir()))

	ctx, cancel := context.WithCancel(context.Background())
	body := `{"id":1,"method":"discover","params":{"path":` + strconv.Quote(dirs[0]) + `}}`
	req, _ := http.NewRequestWithContext(ctx, "POST", ts.URL+"/rpc", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitGlance-Token", testToken)
	clientDone := make(chan error, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
		}
		clientDone <- err
	}()
	waitStart(t, g)
	cancel()
	if err := <-clientDone; err == nil {
		t.Fatal("client was not cancelled")
	}
	time.Sleep(200 * time.Millisecond) // let the server notice the disconnect
	g.release <- struct{}{}
	select {
	case err := <-g.ctxErr:
		if err == nil {
			t.Fatal("the read went on after its client had gone")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the read never reached the end of git")
	}
}

type served struct {
	url, base, tok string
	port           string
	cancel         context.CancelFunc
	done           chan error
}

func startServe(t *testing.T, h *rpc.Handler, open bool) *served {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s := &served{cancel: cancel, done: make(chan error, 1)}
	urlCh := make(chan string, 1)
	go func() { s.done <- Serve(ctx, 0, h, open, func(u string) { urlCh <- u }) }()
	s.url = <-urlCh
	if !strings.HasPrefix(s.url, "http://127.0.0.1:") || !strings.Contains(s.url, "/#token=") || strings.Contains(s.url, "?") {
		t.Fatalf("url = %q", s.url)
	}
	s.tok = s.url[strings.Index(s.url, "#token=")+7:]
	if len(s.tok) != 64 {
		t.Fatalf("token length %d", len(s.tok))
	}
	s.base = s.url[:strings.Index(s.url, "/#")]
	s.port = s.base[strings.LastIndex(s.base, ":")+1:]
	t.Cleanup(cancel)
	return s
}

func (s *served) waitReturn(t *testing.T, within time.Duration) {
	t.Helper()
	select {
	case err := <-s.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(within):
		t.Fatal("Serve did not return")
	}
}

func TestServeShutdownLetsWriteFinish(t *testing.T) {
	dirs := gateDirs(t)
	g := gatedRunner()
	store := t.TempDir()
	h := newHandlerAt(t, g, store)

	openCalls := 0
	old := openBrowser
	openBrowser = func(string) error { openCalls++; return nil }
	defer func() { openBrowser = old }()

	s := startServe(t, h, false)
	status := make(chan int, 1)
	go func() {
		req, _ := http.NewRequest("POST", s.base+"/rpc", strings.NewReader(addRepoReq(1, dirs[0])))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-GitGlance-Token", s.tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			status <- -1
			return
		}
		resp.Body.Close()
		status <- resp.StatusCode
	}()
	waitStart(t, g)
	s.cancel()
	select {
	case <-s.done:
		t.Fatal("Serve returned while a write was running")
	case <-time.After(200 * time.Millisecond):
	}
	g.release <- struct{}{}
	if c := <-status; c != 200 {
		t.Fatalf("write status %d", c)
	}
	s.waitReturn(t, 5*time.Second)
	if openCalls != 0 {
		t.Fatal("browser opened with open=false")
	}
	gl, err := settings.Store{Dir: store}.Global()
	if err != nil || len(gl.Repos) != 1 {
		t.Fatalf("write did not take effect: %+v %v", gl, err)
	}
}

func TestShutdownNotBlockedByUnsentBody(t *testing.T) {
	old := readTimeout
	readTimeout = 400 * time.Millisecond
	defer func() { readTimeout = old }()

	// Each request declares a 100 byte body and never sends it.
	cases := map[string]string{
		"rpc without token":    "POST /rpc HTTP/1.1\r\nHost: %h\r\nContent-Type: application/json\r\n",
		"rpc with token":       "POST /rpc HTTP/1.1\r\nHost: %h\r\nContent-Type: application/json\r\nX-GitGlance-Token: %t\r\n",
		"post to static (405)": "POST / HTTP/1.1\r\nHost: %h\r\n",
		"rpc with bad host":    "POST /rpc HTTP/1.1\r\nHost: evil.example\r\nContent-Type: application/json\r\n",
		"get with a body":      "GET / HTTP/1.1\r\nHost: %h\r\n",
		"options star":         "OPTIONS * HTTP/1.1\r\nHost: %h\r\n",
	}
	for name, head := range cases {
		t.Run(name, func(t *testing.T) {
			s := startServe(t, newHandler(t, gitx.ExecRunner{}), false)
			conn, err := net.Dial("tcp", "127.0.0.1:"+s.port)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			head = strings.NewReplacer("%h", "127.0.0.1:"+s.port, "%t", s.tok).Replace(head)
			io.WriteString(conn, head+"Content-Length: 100\r\n\r\n")
			time.Sleep(100 * time.Millisecond)
			s.cancel()
			s.waitReturn(t, 3*time.Second)
		})
	}
}

// The read timeout covers reading the request only: a handler that takes
// longer than it still answers.
func TestSlowHandlerNotCutByReadTimeout(t *testing.T) {
	old := readTimeout
	readTimeout = 300 * time.Millisecond
	defer func() { readTimeout = old }()
	dirs := gateDirs(t)
	g := gatedRunner()
	s := startServe(t, newHandler(t, g), false)
	status := make(chan int, 1)
	go func() {
		req, _ := http.NewRequest("POST", s.base+"/rpc", strings.NewReader(addRepoReq(1, dirs[0])))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-GitGlance-Token", s.tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			status <- -1
			return
		}
		resp.Body.Close()
		status <- resp.StatusCode
	}()
	waitStart(t, g)
	time.Sleep(900 * time.Millisecond)
	g.release <- struct{}{}
	if c := <-status; c != 200 {
		t.Fatalf("slow write answered %d", c)
	}
}

func TestServeOpensBrowserThroughLaunchFile(t *testing.T) {
	var arg string
	var page []byte
	old := openBrowser
	openBrowser = func(a string) error {
		arg = a
		page, _ = os.ReadFile(a)
		if fi, err := os.Stat(a); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600) {
			t.Errorf("launch file mode %v %v", fi, err)
		}
		return nil
	}
	defer func() { openBrowser = old }()
	s := startServe(t, newHandler(t, gitx.ExecRunner{}), true)
	s.cancel()
	s.waitReturn(t, 5*time.Second)
	if arg == "" {
		t.Fatal("opener was not called")
	}
	if strings.Contains(arg, s.tok) {
		t.Fatalf("opener argument contains the token: %q", arg)
	}
	ps := string(page)
	if !strings.Contains(ps, s.url) || !strings.Contains(ps, "http-equiv=\"refresh\"") || !strings.Contains(ps, "location.replace(") {
		t.Fatalf("launch file: %q", ps)
	}
	if _, err := os.Stat(arg); !os.IsNotExist(err) {
		t.Fatalf("launch file still there: %v", err)
	}
	if _, err := os.Stat(arg[:strings.LastIndexAny(arg, `/\`)]); !os.IsNotExist(err) {
		t.Fatalf("launch dir still there: %v", err)
	}
}
