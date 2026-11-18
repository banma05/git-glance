// Package web serves the UI and the rpc methods to the user's own browser on
// 127.0.0.1. Every web page the user visits can reach a local port (through
// cross-site requests and DNS rebinding), so each request is refused unless it
// comes to a known host name and, for rpc, proves it holds the token and was
// sent by this page and not by a form or another site. There are no cookies:
// the token lives in the URL fragment, which no request ever carries.
package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/banma05/git-glance/core/internal/rpc"
)

//go:embed all:dist
var distFS embed.FS

const (
	tokenHeader = "X-GitGlance-Token"
	maxBody     = 1 << 20
)

// openBrowser starts the system browser on a local file, without a shell.
// The token is never an argument. Tests replace it.
var openBrowser = func(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	case "darwin":
		cmd = exec.Command("open", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// readTimeout bounds how long a client may take to send a whole request, the
// body drain after a refusal included. net/http lifts it once the body has
// been read, so a handler that runs longer than this still answers.
var readTimeout = 10 * time.Second

// NewServer returns the HTTP handler: static files from ui and POST /rpc.
// Static files hold no data, so they need only an allowed Host. POST /rpc
// needs the token in the X-GitGlance-Token header.
// The port every host and origin check uses is the port the request really
// arrived on, so it also holds when the OS picked the port.
// NewServer panics on an empty token, which would let every caller in.
func NewServer(h *rpc.Handler, ui fs.FS, token string) http.Handler {
	if token == "" {
		panic("web.NewServer: empty token")
	}
	s := &server{h: h, token: token, static: http.FileServerFS(ui)}
	return http.HandlerFunc(s.serve)
}

type server struct {
	h      *rpc.Handler
	token  string
	static http.Handler
}

func (s *server) serve(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w.Header())

	port, ok := localPort(r)
	if !ok || !hostAllowed(r.Host, port) {
		refuse(w, http.StatusForbidden, "forbidden")
		return
	}
	if r.URL.Path == "/rpc" {
		s.serveRPC(w, r, port)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		refuse(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.static.ServeHTTP(&headerGuard{ResponseWriter: w}, r)
}

func setSecurityHeaders(hd http.Header) {
	hd.Set("Referrer-Policy", "no-referrer")
	hd.Set("Cache-Control", "no-store")
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Content-Security-Policy", "frame-ancestors 'none'")
}

// headerGuard puts the security headers back right before the status line is
// written, because the file server's error path strips some headers.
type headerGuard struct{ http.ResponseWriter }

func (g *headerGuard) WriteHeader(code int) {
	setSecurityHeaders(g.ResponseWriter.Header())
	g.ResponseWriter.WriteHeader(code)
}

// refuse answers and closes the connection, so a client that announced a body
// and never sends it cannot keep the connection (or a shutdown) waiting.
func refuse(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Connection", "close")
	http.Error(w, msg, code)
}

func (s *server) serveRPC(w http.ResponseWriter, r *http.Request, port string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		refuse(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.valid(r.Header.Get(tokenHeader)) {
		refuse(w, http.StatusForbidden, "forbidden")
		return
	}
	if !isJSON(r.Header.Get("Content-Type")) {
		refuse(w, http.StatusUnsupportedMediaType, "content type must be application/json")
		return
	}
	if o := r.Header.Get("Origin"); o != "" && !originAllowed(o, port) {
		refuse(w, http.StatusForbidden, "forbidden")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			refuse(w, http.StatusRequestEntityTooLarge, "request body is larger than 1 MiB")
			return
		}
		refuse(w, http.StatusBadRequest, "cannot read the request body")
		return
	}
	var req rpc.Request
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, rpc.Response{Error: &rpc.RPCError{Code: "bad-params", Message: "request is not valid JSON"}})
		return
	}
	// Reads stop with the client. A write that has started is finished by the
	// handler's worker whatever happens to the client.
	writeJSON(w, http.StatusOK, s.h.Do(r.Context(), req))
}

func writeJSON(w http.ResponseWriter, status int, resp rpc.Response) {
	b, err := json.Marshal(resp)
	if err != nil {
		status = http.StatusInternalServerError
		b, _ = json.Marshal(rpc.Response{ID: resp.ID, Error: &rpc.RPCError{Code: "internal", Message: "encoding the result failed"}})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(b)
}

func (s *server) valid(candidate string) bool {
	return candidate != "" && subtle.ConstantTimeCompare([]byte(candidate), []byte(s.token)) == 1
}

func localPort(r *http.Request) (string, bool) {
	addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok {
		return "", false
	}
	_, p, err := net.SplitHostPort(addr.String())
	return p, err == nil
}

func hostAllowed(host, port string) bool {
	return host == "127.0.0.1:"+port || host == "localhost:"+port
}

func originAllowed(origin, port string) bool {
	return origin == "http://127.0.0.1:"+port || origin == "http://localhost:"+port
}

func isJSON(ct string) bool {
	mt, _, _ := strings.Cut(ct, ";")
	return strings.EqualFold(strings.TrimSpace(mt), "application/json")
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// hardenServer sets the http.Server options every listener of ours needs:
// OPTIONS * must reach the handler (and its Host check) instead of being
// answered 200 by net/http, and slow headers are cut off.
func hardenServer(srv *http.Server) {
	srv.DisableGeneralOptionsHandler = true
	srv.ReadHeaderTimeout = 10 * time.Second
	srv.ReadTimeout = readTimeout
}

// launchFile writes an HTML page that sends the browser to url, in a private
// temp dir. The opener gets the page's path, so the token is never on a
// command line. The returned function removes the page and its dir.
func launchFile(url string) (path string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "gitglance-")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { os.RemoveAll(dir) }
	js, _ := json.Marshal(url)
	page := fmt.Sprintf("<!doctype html>\n<meta charset=\"utf-8\">\n"+
		"<meta http-equiv=\"refresh\" content=\"0; url=%s\">\n"+
		"<script>location.replace(%s)</script>\n", html.EscapeString(url), js)
	path = filepath.Join(dir, "open.html")
	if err := os.WriteFile(path, []byte(page), 0o600); err != nil {
		cleanup()
		return "", nil, err
	}
	return path, cleanup, nil
}

// Serve listens on 127.0.0.1:port (0 lets the OS choose), calls ready once with
// the address that carries the token in its fragment, optionally opens the
// browser, and blocks until ctx is done. It then stops accepting and waits for
// requests in flight.
func Serve(ctx context.Context, port int, h *rpc.Handler, open bool, ready func(url string)) error {
	token, err := newToken()
	if err != nil {
		return err
	}
	ui, err := fs.Sub(distFS, "dist")
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: NewServer(h, ui, token)}
	hardenServer(srv)
	url := fmt.Sprintf("http://127.0.0.1:%d/#token=%s", ln.Addr().(*net.TCPAddr).Port, token)
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	if ready != nil {
		ready(url)
	}
	if open {
		path, cleanup, err := launchFile(url)
		if err == nil {
			defer cleanup()
			err = openBrowser(path)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "gitglance: could not open the browser; open the printed address yourself")
		}
	}
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	if err := srv.Shutdown(context.Background()); err != nil {
		return err
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
