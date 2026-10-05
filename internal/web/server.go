// Package web serves the dashboard: one HTML page, its static assets, and the
// JSON and event-stream endpoints the page reads.
package web

import (
	"embed"
	"html/template"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/runlevel-six/stormkeep/internal/archive"
	"github.com/runlevel-six/stormkeep/internal/live"
)

//go:embed static
var staticFS embed.FS

//go:embed index.html
var indexHTML string

// Config is everything the server needs.
type Config struct {
	Name     string  // shown as the page title
	Units    string  // "us" or "metric": what the page shows until the viewer picks
	Altitude float64 // meters, for sea-level pressure
	Version  string

	// AllowFrom, when not empty, limits who may load anything but /healthz to
	// these source networks. See docs/explanation/design.md: on the host
	// network the listener is reachable from the whole LAN, and this is what
	// keeps the reverse proxy the only way in.
	AllowFrom []netip.Prefix

	// AllowIndexing drops the noindex header, for a public dashboard that
	// should appear in search results.
	AllowIndexing bool

	Archive *archive.Archive
	Station *live.Station
	Log     *slog.Logger

	// Done is closed when the server is shutting down, so open event streams
	// end instead of holding the shutdown up.
	Done <-chan struct{}
}

type server struct {
	cfg   Config
	index *template.Template
	now   func() time.Time

	mu    sync.Mutex
	cache map[string]cached

	lastRefused atomic.Int64
}

type cached struct {
	at   time.Time
	body []byte
}

// Handler returns the dashboard's HTTP handler.
func Handler(cfg Config) http.Handler {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	s := &server{
		cfg:   cfg,
		index: template.Must(template.New("index").Parse(indexHTML)),
		now:   time.Now,
		cache: map[string]cached{},
	}
	static, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err) // the embed directive guarantees the directory
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.Handle("GET /static/", cacheForever(http.StripPrefix("/static/", http.FileServerFS(static))))
	mux.HandleFunc("GET /api/now", s.handleNow)
	mux.HandleFunc("GET /api/history", s.handleHistory)
	mux.HandleFunc("GET /api/stream", s.handleStream)
	return s.allow(s.headers(mux))
}

func (s *server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	err := s.index.Execute(w, map[string]string{
		"Name":    s.cfg.Name,
		"Units":   s.cfg.Units,
		"Version": s.cfg.Version,
	})
	if err != nil {
		s.cfg.Log.Error("render index", "err", err)
	}
}

// Static asset URLs carry the build version (?v=), so they can be cached for
// good: a new build changes every URL.
func cacheForever(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		h.ServeHTTP(w, r)
	})
}

func (s *server) headers(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; "+
			"connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "no-referrer")
		if !s.cfg.AllowIndexing {
			hd.Set("X-Robots-Tag", "noindex, nofollow")
		}
		h.ServeHTTP(w, r)
	})
}

// allow refuses requests from outside AllowFrom. It looks only at the TCP
// peer, never at X-Forwarded-For: a header is whatever the client says, and
// the point is to tell the proxy apart from everyone else.
func (s *server) allow(h http.Handler) http.Handler {
	if len(s.cfg.AllowFrom) == 0 {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || s.allowed(r.RemoteAddr) {
			h.ServeHTTP(w, r)
			return
		}
		// Logged, but at most once a minute: when the allowlist is wrong the
		// first refusal says what address the proxy really connects from.
		if now := time.Now().Unix(); now-s.lastRefused.Load() >= 60 {
			s.lastRefused.Store(now)
			s.cfg.Log.Warn("refused a request from outside WX_ALLOW_FROM (logged at most once a minute)", "remote", r.RemoteAddr, "path", r.URL.Path)
		}
		http.Error(w, "forbidden", http.StatusForbidden)
	})
}

func (s *server) allowed(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return false
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, p := range s.cfg.AllowFrom {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
