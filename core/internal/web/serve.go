// Package web serves the UI and the rpc methods to the user's own browser on
// 127.0.0.1. Every web page the user visits can reach a local port (through
// cross-site requests and DNS rebinding), so each request is refused unless it
// proves it holds the token, comes to a known host name and, for rpc, was sent
// by this page and not by a form or another site.
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
	"io"
	"io/fs"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/banma05/git-glance/core/internal/rpc"
)

//go:embed all:dist
var distFS embed.FS

const (
	cookieName  = "gg_token"
	tokenHeader = "X-GitGlance-Token"
	maxBody     = 1 << 20
)

// openBrowser starts the system browser without a shell. Tests replace it.
var openBrowser = func(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// NewServer returns the HTTP handler: static files from ui and POST /rpc.
// The port every host and origin check uses is the port the request really
// arrived on, so it also holds when the OS picked the port.
func NewServer(h *rpc.Handler, ui fs.FS, token string) http.Handler {
	s := &server{h: h, token: token, static: http.FileServerFS(ui)}
	return http.HandlerFunc(s.serve)
}

type server struct {
	h      *rpc.Handler
	token  string
	static http.Handler
}

func (s *server) serve(w http.ResponseWriter, r *http.Request) {
	hd := w.Header()
	setSecurityHeaders(hd)

	port, ok := localPort(r)
	if !ok || !hostAllowed(r.Host, port) {
		forbid(w)
		return
	}
	if r.URL.Path == "/rpc" {
		s.serveRPC(w, r, port)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		hd.Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if q := r.URL.Query().Get("token"); q != "" {
		if !s.valid(q) {
			forbid(w)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name: cookieName, Value: s.token, Path: "/",
			HttpOnly: true, SameSite: http.SameSiteStrictMode,
		})
	} else if !s.authorized(r) {
		forbid(w)
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

func (s *server) serveRPC(w http.ResponseWriter, r *http.Request, port string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(r) {
		forbid(w)
		return
	}
	if !isJSON(r.Header.Get("Content-Type")) {
		http.Error(w, "content type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	if o := r.Header.Get("Origin"); o != "" && !originAllowed(o, port) {
		forbid(w)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, "request body is larger than 1 MiB", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "cannot read the request body", http.StatusBadRequest)
		return
	}
	var req rpc.Request
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, rpc.Response{Error: &rpc.RPCError{Code: "bad-params", Message: "request is not valid JSON"}})
		return
	}
	ctx := r.Context()
	// A write that was accepted runs to its end even if the client leaves or
	// the server is shutting down.
	ctx = context.WithoutCancel(ctx)
	writeJSON(w, http.StatusOK, s.h.Do(ctx, req))
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

func forbid(w http.ResponseWriter) { http.Error(w, "forbidden", http.StatusForbidden) }

func (s *server) valid(candidate string) bool {
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(s.token)) == 1
}

// authorized accepts the cookie or the header.
func (s *server) authorized(r *http.Request) bool {
	if c, err := r.Cookie(cookieName); err == nil && s.valid(c.Value) {
		return true
	}
	if v := r.Header.Get(tokenHeader); v != "" && s.valid(v) {
		return true
	}
	return false
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

// Serve listens on 127.0.0.1:port (0 lets the OS choose), calls ready once with
// the address that carries the token, optionally opens the browser, and blocks
// until ctx is done. It then stops accepting and waits for requests in flight.
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
	srv := &http.Server{
		Handler:           NewServer(h, ui, token),
		ReadHeaderTimeout: 10 * time.Second,
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/?token=%s", ln.Addr().(*net.TCPAddr).Port, token)
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	if ready != nil {
		ready(url)
	}
	if open {
		openBrowser(url) // the printed address still works when this fails
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
