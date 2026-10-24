package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/floriandejonckheere/dropcatch/internal/auth"
)

// requireAuth rejects API requests without a valid session. The UI shell
// itself is public: it contains no data and shows the login form.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	if s.auth == nil {
		return next
	}
	public := map[string]bool{"/api/config": true, "/api/login": true, "/api/logout": true}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && !public[r.URL.Path] && !s.auth.Valid(r) {
			writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"error": "login required", "code": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	cookie, err := s.auth.Login(auth.ClientIP(r), req.Username, req.Password, isSecure(r))
	switch {
	case errors.Is(err, auth.ErrRateLimited):
		writeError(w, http.StatusTooManyRequests, err.Error())
	case err != nil:
		writeError(w, http.StatusUnauthorized, "wrong username or password")
	default:
		cookie.Path = s.cfg.BasePath
		http.SetCookie(w, cookie)
		writeJSON(w, map[string]bool{"ok": true})
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if s.auth != nil {
		s.auth.Revoke(r)
	}
	c := auth.Logout(isSecure(r))
	c.Path = s.cfg.BasePath
	http.SetCookie(w, c)
	writeJSON(w, map[string]bool{"ok": true})
}

// isSecure reports whether the client connection uses HTTPS, directly or
// through a reverse proxy.
func isSecure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
