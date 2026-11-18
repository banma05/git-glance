package web

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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

func newHandler(t *testing.T, r gitx.Runner) *rpc.Handler {
	t.Helper()
	h, err := rpc.NewHandler(r, settings.Store{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func startWeb(t *testing.T, h *rpc.Handler) *httptest.Server {
	t.Helper()
	ui := fstest.MapFS{
		"index.html": {Data: []byte("<html>index</html>")},
		"app.js":     {Data: []byte("console.log(1)")},
	}
	ts := httptest.NewServer(NewServer(h, ui, testToken))
	t.Cleanup(ts.Close)
	return ts
}

func plain(t *testing.T) *httptest.Server {
	return startWeb(t, newHandler(t, gitx.ExecRunner{}))
}

type opt func(*http.Request)

func withCookie(tok string) opt {
	return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "gg_token", Value: tok}) }
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

func rpcPost(t *testing.T, ts *httptest.Server, body string, opts ...opt) *http.Response {
	t.Helper()
	all := append([]opt{withCookie(testToken), withHeader("Content-Type", "application/json")}, opts...)
	return do(t, ts, "POST", "/rpc", body, all...)
}

const versionReq = `{"id":1,"method":"version"}`

func TestNoTokenForbidden(t *testing.T) {
	ts := plain(t)
	if c := do(t, ts, "GET", "/", "").StatusCode; c != 403 {
		t.Fatalf("GET / = %d", c)
	}
	if c := do(t, ts, "POST", "/rpc", versionReq, withHeader("Content-Type", "application/json")).StatusCode; c != 403 {
		t.Fatalf("POST /rpc = %d", c)
	}
}

func TestTokenFlow(t *testing.T) {
	ts := plain(t)
	resp := do(t, ts, "GET", "/?token="+testToken, "")
	if resp.StatusCode != 200 {
		t.Fatalf("GET /?token = %d", resp.StatusCode)
	}
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "gg_token" {
			cookie = c
		}
	}
	if cookie == nil || cookie.Value != testToken || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
		t.Fatalf("bad cookie: %+v", cookie)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "index") {
		t.Fatalf("body = %q", b)
	}
	r := rpcPost(t, ts, versionReq, withCookie(cookie.Value))
	rb, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 || !strings.Contains(string(rb), `"gitOK":true`) {
		t.Fatalf("rpc = %d %s", r.StatusCode, rb)
	}
	// The header works in place of the cookie.
	req, _ := http.NewRequest("POST", ts.URL+"/rpc", strings.NewReader(versionReq))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("X-GitGlance-Token", testToken)
	r2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Body.Close()
	if r2.StatusCode != 200 {
		t.Fatalf("header token rpc = %d", r2.StatusCode)
	}
}

func TestBadHost(t *testing.T) {
	ts := plain(t)
	if c := do(t, ts, "GET", "/?token="+testToken, "", withHost("evil.example:1234")).StatusCode; c != 403 {
		t.Fatalf("evil host = %d", c)
	}
	if c := rpcPost(t, ts, versionReq, withHost("evil.example")).StatusCode; c != 403 {
		t.Fatalf("host without port = %d", c)
	}
	addr := ts.Listener.Addr().String()
	port := addr[strings.LastIndex(addr, ":")+1:]
	if c := do(t, ts, "GET", "/?token="+testToken, "", withHost("localhost:"+port)).StatusCode; c != 200 {
		t.Fatalf("localhost host = %d", c)
	}
	if c := do(t, ts, "GET", "/?token="+testToken, "", withHost("127.0.0.1:1")).StatusCode; c != 403 {
		t.Fatalf("wrong port = %d", c)
	}
}

func TestForeignOriginForbidden(t *testing.T) {
	ts := plain(t)
	if c := rpcPost(t, ts, versionReq, withHeader("Origin", "http://evil.example")).StatusCode; c != 403 {
		t.Fatalf("foreign origin = %d", c)
	}
	if c := rpcPost(t, ts, versionReq, withHeader("Origin", "http://"+ts.Listener.Addr().String())).StatusCode; c != 200 {
		t.Fatalf("own origin = %d", c)
	}
}

func TestContentTypeRequired(t *testing.T) {
	ts := plain(t)
	resp := do(t, ts, "POST", "/rpc", versionReq, withCookie(testToken), withHeader("Content-Type", "text/plain"))
	if resp.StatusCode != 403 && resp.StatusCode != 415 {
		t.Fatalf("text/plain = %d", resp.StatusCode)
	}
	resp = do(t, ts, "POST", "/rpc", versionReq, withCookie(testToken))
	if resp.StatusCode != 403 && resp.StatusCode != 415 {
		t.Fatalf("no content type = %d", resp.StatusCode)
	}
}

func TestWrongTokens(t *testing.T) {
	ts := plain(t)
	off := testToken[:len(testToken)-1] + "0"
	for _, tok := range []string{"nope", off, ""} {
		if c := do(t, ts, "POST", "/rpc", versionReq, withCookie(tok), withHeader("Content-Type", "application/json")).StatusCode; c != 403 {
			t.Fatalf("cookie %q = %d", tok, c)
		}
		if c := do(t, ts, "GET", "/?token="+tok, "").StatusCode; c != 403 {
			t.Fatalf("query %q = %d", tok, c)
		}
	}
	resp := do(t, ts, "POST", "/rpc", versionReq, withHeader("Content-Type", "application/json"), withHeader("X-GitGlance-Token", off))
	if resp.StatusCode != 403 {
		t.Fatalf("header off by one = %d", resp.StatusCode)
	}
}

func TestStaticNeedsCookie(t *testing.T) {
	ts := plain(t)
	if c := do(t, ts, "GET", "/app.js", "").StatusCode; c != 403 {
		t.Fatalf("no cookie = %d", c)
	}
	if c := do(t, ts, "GET", "/app.js", "", withCookie(testToken)).StatusCode; c != 200 {
		t.Fatalf("cookie = %d", c)
	}
	if c := do(t, ts, "HEAD", "/app.js", "", withCookie(testToken)).StatusCode; c != 200 {
		t.Fatalf("HEAD = %d", c)
	}
	if c := do(t, ts, "POST", "/app.js", "x", withCookie(testToken)).StatusCode; c != 405 {
		t.Fatalf("POST static = %d", c)
	}
	if c := do(t, ts, "GET", "/missing.js", "", withCookie(testToken)).StatusCode; c != 404 {
		t.Fatalf("missing = %d", c)
	}
}

func TestMethodsOnRPC(t *testing.T) {
	ts := plain(t)
	for _, m := range []string{"GET", "PUT", "DELETE", "OPTIONS"} {
		if c := do(t, ts, m, "/rpc", "", withCookie(testToken)).StatusCode; c != 405 {
			t.Fatalf("%s /rpc = %d", m, c)
		}
	}
}

func TestSecurityHeadersAndNoCORS(t *testing.T) {
	ts := plain(t)
	resps := []*http.Response{
		do(t, ts, "GET", "/", ""),
		do(t, ts, "GET", "/", "", withHost("evil.example:1")),
		do(t, ts, "GET", "/?token="+testToken, ""),
		rpcPost(t, ts, versionReq),
		do(t, ts, "GET", "/rpc", ""),
		do(t, ts, "OPTIONS", "/rpc", "", withHeader("Origin", "http://evil")),
		do(t, ts, "GET", "/nothing", "", withCookie(testToken)),
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
	}
	if resps[0].StatusCode != 403 || resps[2].StatusCode != 200 {
		t.Fatalf("unexpected statuses %d %d", resps[0].StatusCode, resps[2].StatusCode)
	}
}

func TestTokenNeverEchoed(t *testing.T) {
	ts := plain(t)
	for _, r := range []*http.Response{
		do(t, ts, "GET", "/?token="+testToken[:10], ""),
		rpcPost(t, ts, versionReq, withHeader("Origin", "http://evil.example")),
		do(t, ts, "POST", "/rpc", `{"bad`, withCookie(testToken[:20]), withHeader("Content-Type", "application/json")),
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

// gateRunner blocks every rev-parse made in a "gate-" folder (addRepo makes
// one) until released, and reports which folder reached git.
type gateRunner struct {
	gitx.Runner
	started chan string
	release chan struct{}
}

func (g *gateRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if len(args) > 0 && args[0] == "rev-parse" && strings.Contains(dir, "gate-") {
		g.started <- dir
		<-g.release
	}
	return g.Runner.Run(ctx, dir, args...)
}

func addRepoReq(id int, dir string) string {
	return `{"id":` + strconv.Itoa(id) + `,"method":"addRepo","params":{"path":` + strconv.Quote(dir) + `}}`
}

func gatedRunner() *gateRunner {
	return &gateRunner{Runner: gitx.ExecRunner{}, started: make(chan string, 8), release: make(chan struct{}, 8)}
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

func TestServeShutdownLetsWriteFinish(t *testing.T) {
	dirs := gateDirs(t)
	g := gatedRunner()
	h := newHandler(t, g)

	openCalls := 0
	old := openBrowser
	openBrowser = func(string) error { openCalls++; return nil }
	defer func() { openBrowser = old }()

	ctx, cancel := context.WithCancel(context.Background())
	urlCh := make(chan string, 1)
	served := make(chan error, 1)
	go func() { served <- Serve(ctx, 0, h, false, func(u string) { urlCh <- u }) }()
	u := <-urlCh
	if !strings.HasPrefix(u, "http://127.0.0.1:") || !strings.Contains(u, "/?token=") {
		t.Fatalf("url = %q", u)
	}
	tok := u[strings.Index(u, "token=")+6:]
	if len(tok) != 64 {
		t.Fatalf("token length %d", len(tok))
	}
	base := u[:strings.Index(u, "/?")]

	status := make(chan int, 1)
	go func() {
		req, _ := http.NewRequest("POST", base+"/rpc", strings.NewReader(addRepoReq(1, dirs[0])))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-GitGlance-Token", tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			status <- -1
			return
		}
		resp.Body.Close()
		status <- resp.StatusCode
	}()
	waitStart(t, g)
	cancel()
	select {
	case <-served:
		t.Fatal("Serve returned while a write was running")
	case <-time.After(200 * time.Millisecond):
	}
	g.release <- struct{}{}
	if c := <-status; c != 200 {
		t.Fatalf("write status %d", c)
	}
	select {
	case err := <-served:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return")
	}
	if openCalls != 0 {
		t.Fatal("browser opened with open=false")
	}
}

func TestServeOpensBrowserWhenAsked(t *testing.T) {
	var opened string
	old := openBrowser
	openBrowser = func(u string) error { opened = u; return nil }
	defer func() { openBrowser = old }()
	ctx, cancel := context.WithCancel(context.Background())
	urlCh := make(chan string, 1)
	served := make(chan error, 1)
	go func() { served <- Serve(ctx, 0, newHandler(t, gitx.ExecRunner{}), true, func(u string) { urlCh <- u }) }()
	u := <-urlCh
	cancel()
	<-served
	if opened != u {
		t.Fatalf("opened %q, want %q", opened, u)
	}
}
