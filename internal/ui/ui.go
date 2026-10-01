// Package ui serves Transom's control panel — a small embedded web app
// on 127.0.0.1 — for a browser tab or the native Transom.app window.
//
// Every API call is a POST with the X-Transom-Token header and answers
// {"ok": true, "data": ...} or {"ok": false, "error": "..."}. Requests
// whose Host isn't 127.0.0.1/localhost are refused (DNS rebinding).
package ui

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
	"path"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"transom/internal/version"
)

//go:embed all:web
var webFS embed.FS

// TokenHeader carries the per-run secret on every API call.
const TokenHeader = "X-Transom-Token"

// maxBody caps API request bodies.
const maxBody = 1 << 20

// startPanel serves the panel on an ephemeral 127.0.0.1 port. Callers
// shut srv down. lastSeen is the unix time of the latest request.
func startPanel() (srv *http.Server, url string, lastSeen *atomic.Int64, err error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", nil, err
	}
	token, err := newToken()
	if err != nil {
		ln.Close()
		return nil, "", nil, err
	}
	lastSeen = &atomic.Int64{}
	lastSeen.Store(time.Now().Unix())
	s := newServer()
	h := newHandlerWith(token, s)
	srv = &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			lastSeen.Store(time.Now().Unix())
			h.ServeHTTP(w, r)
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	srv.RegisterOnShutdown(s.stop)
	go srv.Serve(ln)
	url = fmt.Sprintf("http://%s/?t=%s", ln.Addr().String(), token)
	return srv, url, lastSeen, nil
}

// Run serves the panel, opens it in the default browser, and blocks
// until ctx is cancelled — or, when idleExit > 0, until no request has
// arrived for that long (the tab was closed).
func Run(ctx context.Context, idleExit time.Duration, out io.Writer) error {
	srv, url, lastSeen, err := startPanel()
	if err != nil {
		return err
	}
	defer srv.Shutdown(context.Background())
	if out != nil {
		fmt.Fprintln(out, "Control panel:", url)
	}
	if err := openBrowser(url); err != nil && out != nil {
		fmt.Fprintln(out, "Couldn't open a browser; open the URL above.")
	}
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			if idleExit > 0 && time.Since(time.Unix(lastSeen.Load(), 0)) > idleExit {
				return nil
			}
		}
	}
}

// RunDesktop shares the exact browser panel with a native desktop window.
// The window owns the lifetime of the HTTP server and its scan jobs.
func RunDesktop(ctx context.Context, window func(context.Context, string) error) error {
	srv, url, _, err := startPanel()
	if err != nil {
		return err
	}
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if srv.Shutdown(shutdown) != nil {
			_ = srv.Close()
		}
	}()
	return window(ctx, url)
}

// RunHost serves the panel for the native Transom.app, which runs
// `transom ui --window-host` as a child: exactly one line,
// "TRANSOM_UI_URL=<url>", goes to out, then it serves until stdin
// reaches EOF (the app exited) or ctx is cancelled.
func RunHost(ctx context.Context, stdin io.Reader, out io.Writer) error {
	srv, url, _, err := startPanel()
	if err != nil {
		return err
	}
	defer srv.Shutdown(context.Background())
	fmt.Fprintf(out, "TRANSOM_UI_URL=%s\n", url)

	stdinClosed := make(chan struct{})
	go func() {
		io.Copy(io.Discard, stdin)
		close(stdinClosed)
	}()
	select {
	case <-stdinClosed:
	case <-ctx.Done():
	}
	return nil
}

func newToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// hostAllowed guards against DNS rebinding: a page on evil.example that
// rebinds its name to 127.0.0.1 still sends Host: evil.example.
func hostAllowed(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	return h == "127.0.0.1" || strings.EqualFold(h, "localhost")
}

// apiFunc handles one endpoint: body is the raw JSON request.
type apiFunc func(ctx context.Context, body []byte) (any, error)

// newHandler builds the whole HTTP surface around one token.
func newHandler(token string) http.Handler {
	return newHandlerWith(token, newServer())
}

func newHandlerWith(token string, s *server) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		serveStatic(w, r, "index.html")
	})
	mux.HandleFunc("/static/", func(w http.ResponseWriter, r *http.Request) {
		serveStatic(w, r, strings.TrimPrefix(r.URL.Path, "/static/"))
	})

	api := func(route string, h apiFunc) {
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get(TokenHeader)), []byte(token)) != 1 {
				writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "bad token"})
				return
			}
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", "POST")
				writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "use POST"})
				return
			}
			body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
			if err == nil && len(body) > maxBody {
				err = errors.New("request too large")
			}
			var data any
			if err == nil {
				data, err = h(r.Context(), body)
			}
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			if data == nil {
				data = struct{}{}
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": data})
		})
	}
	s.register(api)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hostAllowed(r.Host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// contentTypes maps the web assets' extensions explicitly, so they don't
// depend on the machine's mime database.
var contentTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".json":  "application/json; charset=utf-8",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".gif":   "image/gif",
	".svg":   "image/svg+xml",
	".ico":   "image/x-icon",
	".webp":  "image/webp",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
	".txt":   "text/plain; charset=utf-8",
	".map":   "application/json; charset=utf-8",
}

// serveStatic writes web/<name> from the embedded files.
func serveStatic(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	clean := path.Clean("/" + name)[1:]
	if clean == "" || clean != name || !fs.ValidPath(clean) {
		http.NotFound(w, r)
		return
	}
	b, err := fs.ReadFile(webFS, "web/"+clean)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if clean == "index.html" {
		b = platformHTML(b, runtime.GOOS)
	}
	ct, ok := contentTypes[strings.ToLower(path.Ext(clean))]
	if !ok {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		w.Write(b)
	}
}

func platformHTML(b []byte, goos string) []byte {
	name := map[string]string{"darwin": "macOS", "windows": "Windows", "linux": "Linux"}[goos]
	if name == "" {
		name = goos
	}
	return []byte(strings.NewReplacer("{{platform}}", goos, "{{platformName}}", name,
		"{{version}}", version.Number).Replace(string(b)))
}
