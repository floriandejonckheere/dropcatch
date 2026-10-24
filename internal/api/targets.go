package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/floriandejonckheere/dropcatch/internal/monitor"
	"github.com/floriandejonckheere/dropcatch/internal/notify"
	"github.com/floriandejonckheere/dropcatch/internal/store"
)

const maxName = 100

// validateTarget checks a target's name and Apprise URL.
func (s *Server) validateTarget(ctx context.Context, name, url string) error {
	if name == "" || len(name) > maxName {
		return badRequest{errors.New("enter a name of at most 100 characters")}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := s.apprise.Validate(ctx, url); err != nil {
		return badRequest{err}
	}
	return nil
}

func (s *Server) handleTargetAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		URL     string `json:"url"`
		Enabled bool   `json:"enabled"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Name, req.URL = strings.TrimSpace(req.Name), strings.TrimSpace(req.URL)
	if err := s.validateTarget(r.Context(), req.Name, req.URL); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var t store.Target
	if !s.update(w, func(st *store.State) error {
		t = *st.AddTarget(store.Target{Name: req.Name, URL: req.URL, Enabled: req.Enabled})
		return nil
	}) {
		return
	}
	s.publishTargets()
	writeJSON(w, t)
}

func (s *Server) handleTargetEdit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    *string `json:"name"`
		URL     *string `json:"url"`
		Enabled *bool   `json:"enabled"`
	}
	if !decode(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	old := s.store.Snapshot().Target(id)
	if old == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	name, url := old.Name, old.URL
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
	}
	if req.URL != nil {
		url = strings.TrimSpace(*req.URL)
	}
	if name != old.Name || url != old.URL {
		if err := s.validateTarget(r.Context(), name, url); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	var t store.Target
	if !s.update(w, func(st *store.State) error {
		p := st.Target(id)
		if p == nil {
			return store.ErrNotFound
		}
		if p.URL != url {
			// The last result was for another service.
			p.LastSent, p.LastError = nil, ""
		}
		p.Name, p.URL = name, url
		if req.Enabled != nil {
			p.Enabled = *req.Enabled
		}
		t = *p
		return nil
	}) {
		return
	}
	s.publishTargets()
	writeJSON(w, t)
}

func (s *Server) handleTargetRemove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.update(w, func(st *store.State) error { return st.RemoveTarget(id) }) {
		return
	}
	s.publishTargets()
	writeJSON(w, map[string]bool{"ok": true})
}

// handleTargetTest sends a test notification and reports whether it worked.
func (s *Server) handleTargetTest(w http.ResponseWriter, r *http.Request) {
	err := s.monitor.SendTo(r.Context(), r.PathValue("id"), monitor.Message{
		Title: "Dropcatch test notification",
		Body:  "Notifications from Dropcatch reach you here.",
		Kind:  notify.Info,
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case err != nil:
		writeError(w, http.StatusBadGateway, err.Error())
	default:
		writeJSON(w, map[string]bool{"ok": true})
	}
}

func (s *Server) publishTargets() { s.events.publish("targets", s.store.Snapshot().Targets) }
