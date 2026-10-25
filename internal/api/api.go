// Package api implements Dropcatch's HTTP API and serves the web UI.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/floriandejonckheere/dropcatch/internal/auth"
	"github.com/floriandejonckheere/dropcatch/internal/config"
	"github.com/floriandejonckheere/dropcatch/internal/monitor"
	"github.com/floriandejonckheere/dropcatch/internal/notify"
	"github.com/floriandejonckheere/dropcatch/internal/ovh"
	"github.com/floriandejonckheere/dropcatch/internal/store"
)

var Version = "dev"

type Server struct {
	cfg     *config.Config
	store   *store.Store
	monitor *monitor.Monitor
	ovh     OVH
	apprise *notify.Apprise
	auth    *auth.Auth // nil when authentication is disabled
	events  *hub
	ui      fs.FS
}

// OVH reads VPS plans and stock (an *ovh.Client).
type OVH interface {
	Plans(ctx context.Context, subsidiary string) (ovh.Catalog, error)
	Stock(ctx context.Context, subsidiary, plan string) ([]ovh.Datacenter, error)
}

// New wires the server to the monitor's change callbacks.
func New(cfg *config.Config, st *store.Store, m *monitor.Monitor, o OVH, apprise *notify.Apprise, ui fs.FS) (*Server, error) {
	a, err := auth.New(cfg.Username, cfg.Password, cfg.PasswordHash, cfg.Secret, cfg.DataDir)
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, store: st, monitor: m, ovh: o, apprise: apprise, auth: a, events: newHub(), ui: ui}
	m.OnDomain = func(d store.Domain) { s.events.publish("domain", view(d)) }
	m.OnVPS = func(v store.VPS) { s.events.publish("vps", v) }
	m.OnTargets = func() { s.events.publish("targets", s.store.Snapshot().Targets) }
	m.OnChecking = func(names []string) { s.events.publish("checking", names) }
	return s, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Public endpoints.
	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.handleLogout)

	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("GET /api/events", s.handleEvents)

	mux.HandleFunc("POST /api/domains", s.handleDomainAdd)
	mux.HandleFunc("PATCH /api/domains/{name}", s.handleDomainEdit)
	mux.HandleFunc("DELETE /api/domains/{name}", s.handleDomainRemove)
	mux.HandleFunc("POST /api/domains/{name}/check", s.handleDomainCheck)
	mux.HandleFunc("POST /api/check", s.handleCheckAll)

	mux.HandleFunc("GET /api/ovh/plans", s.handleOVHPlans)
	mux.HandleFunc("GET /api/ovh/stock", s.handleOVHStock)
	mux.HandleFunc("POST /api/vps", s.handleVPSAdd)
	mux.HandleFunc("PATCH /api/vps/{id}", s.handleVPSEdit)
	mux.HandleFunc("DELETE /api/vps/{id}", s.handleVPSRemove)
	mux.HandleFunc("POST /api/vps/{id}/check", s.handleVPSCheck)

	mux.HandleFunc("POST /api/targets", s.handleTargetAdd)
	mux.HandleFunc("PATCH /api/targets/{id}", s.handleTargetEdit)
	mux.HandleFunc("DELETE /api/targets/{id}", s.handleTargetRemove)
	mux.HandleFunc("POST /api/targets/{id}/test", s.handleTargetTest)

	mux.HandleFunc("PATCH /api/settings", s.handleSettings)

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	mux.Handle("/", s.uiHandler())

	var h http.Handler = mux
	h = s.requireAuth(h)
	h = sameOrigin(h)
	h = securityHeaders(h)
	if s.cfg.BasePath != "/" {
		h = http.StripPrefix(strings.TrimSuffix(s.cfg.BasePath, "/"), h)
	}
	return h
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"version":         Version,
		"apprise":         s.apprise.Path != "",
		"ovhSubsidiaries": ovh.Subsidiaries,
		"limits": map[string]int{
			"minInterval":      store.MinInterval,
			"maxInterval":      store.MaxInterval,
			"minStockInterval": store.MinStockInterval,
			"maxStockInterval": store.MaxStockInterval,
			"minTimeout":       store.MinTimeout,
			"maxTimeout":       store.MaxTimeout,
		},
		"auth": map[string]bool{
			"required":      s.auth != nil,
			"username":      s.auth != nil && s.auth.RequiresUsername(),
			"authenticated": s.auth == nil || s.auth.Valid(r),
		},
	})
}

// handleState returns everything the UI shows.
func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	st := s.store.Snapshot()
	writeJSON(w, map[string]any{
		"domains":  views(st.Domains),
		"vps":      st.VPS,
		"targets":  st.Targets,
		"settings": st.Settings,
		"checking": s.monitor.Checking(),
	})
}

// update applies fn to the state and writes the error response if it fails.
func (s *Server) update(w http.ResponseWriter, fn func(*store.State) error) bool {
	err := s.store.Update(fn)
	var bad badRequest
	switch {
	case err == nil:
		return true
	case errors.As(err, &bad):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, store.ErrExists):
		writeError(w, http.StatusConflict, err.Error())
	default:
		slog.Error("could not save state", "err", err)
		writeError(w, http.StatusInternalServerError, "could not save the change")
	}
	return false
}

// badRequest is a validation error from a handler's update function.
type badRequest struct{ error }

// decode reads a JSON request body into v.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, v any) { writeJSONStatus(w, http.StatusOK, v) }

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSONStatus(w, status, map[string]string{"error": msg})
}

// csp only allows Dropcatch's own scripts. Inline styles are needed for
// style attributes in Svelte templates.
const csp = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; font-src 'self' data:; connect-src 'self'; " +
	"object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

// sameOrigin rejects cross-site modifying requests (CSRF protection on top
// of SameSite cookies). Browsers always send Origin on cross-origin POSTs.
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if o := r.Header.Get("Origin"); o != "" {
				u, err := url.Parse(o)
				if err != nil || !strings.EqualFold(u.Host, r.Host) {
					writeError(w, http.StatusForbidden, "cross-origin request refused")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) uiHandler() http.Handler {
	files := http.FileServerFS(s.ui)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			// Vite emits content-hashed file names.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		if _, err := fs.Stat(s.ui, strings.TrimPrefix(path.Clean(r.URL.Path), "/")); err != nil {
			r.URL.Path = "/"
		}
		if path.Ext(r.URL.Path) == ".webmanifest" {
			// Not in Go's built-in MIME table, and minimal images lack /etc/mime.types.
			w.Header().Set("Content-Type", "application/manifest+json")
		}
		files.ServeHTTP(w, r)
	})
}
