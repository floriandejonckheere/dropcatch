package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/floriandejonckheere/dropcatch/internal/lookup"
	"github.com/floriandejonckheere/dropcatch/internal/store"
)

// domainView is a domain as sent to the UI.
type domainView struct {
	store.Domain
	Display string `json:"display"` // Unicode form of an internationalised name
}

func view(d store.Domain) domainView { return domainView{Domain: d, Display: lookup.Display(d.Name)} }

func views(ds []store.Domain) []domainView {
	out := make([]domainView, len(ds))
	for i, d := range ds {
		out[i] = view(d)
	}
	return out
}

const maxNote = 500

// handleDomainAdd adds one or more domains, separated by whitespace or commas.
func (s *Server) handleDomainAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Names string `json:"names"`
		Note  string `json:"note"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.Note) > maxNote {
		writeError(w, http.StatusBadRequest, "the note is too long")
		return
	}
	type failure struct {
		Input string `json:"input"`
		Error string `json:"error"`
	}
	failed := []failure{}
	var names []string
	for _, in := range strings.FieldsFunc(req.Names, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\r' || r == '\t' }) {
		name, err := lookup.Normalize(in)
		if err != nil {
			failed = append(failed, failure{in, err.Error()})
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 && len(failed) == 0 {
		writeError(w, http.StatusBadRequest, "enter a domain name")
		return
	}

	added := []domainView{}
	now := time.Now().UTC()
	if !s.update(w, func(st *store.State) error {
		for _, name := range names {
			d, err := st.AddDomain(name, strings.TrimSpace(req.Note), now)
			if errors.Is(err, store.ErrExists) {
				failed = append(failed, failure{name, "already monitored"})
				continue
			}
			added = append(added, view(*d))
		}
		return nil
	}) {
		return
	}
	if len(added) > 0 {
		s.events.publish("domains", views(s.store.Snapshot().Domains))
		check := make([]string, len(added))
		for i, d := range added {
			check[i] = d.Name
		}
		s.monitor.Check(check...)
	}
	writeJSON(w, map[string]any{"added": added, "failed": failed})
}

func (s *Server) handleDomainEdit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Note   *string `json:"note"`
		Paused *bool   `json:"paused"`
	}
	if !decode(w, r, &req) {
		return
	}
	var d store.Domain
	if !s.update(w, func(st *store.State) error {
		p := st.Domain(r.PathValue("name"))
		if p == nil {
			return store.ErrNotFound
		}
		if req.Note != nil {
			if len(*req.Note) > maxNote {
				return badRequest{errors.New("the note is too long")}
			}
			p.Note = strings.TrimSpace(*req.Note)
		}
		if req.Paused != nil {
			p.Paused = *req.Paused
		}
		d = *p
		return nil
	}) {
		return
	}
	s.events.publish("domain", view(d))
	s.monitor.Wake()
	writeJSON(w, view(d))
}

func (s *Server) handleDomainRemove(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !s.update(w, func(st *store.State) error { return st.RemoveDomain(name) }) {
		return
	}
	s.events.publish("removed", name)
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleDomainCheck(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if s.store.Snapshot().Domain(name) == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	s.monitor.Check(name)
	writeJSONStatus(w, http.StatusAccepted, map[string]bool{"ok": true})
}

func (s *Server) handleCheckAll(w http.ResponseWriter, r *http.Request) {
	s.monitor.Check()
	writeJSONStatus(w, http.StatusAccepted, map[string]bool{"ok": true})
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Interval      *int  `json:"interval"`
		Timeout       *int  `json:"timeout"`
		NotifyChanges *bool `json:"notifyChanges"`
	}
	if !decode(w, r, &req) {
		return
	}
	var settings store.Settings
	if !s.update(w, func(st *store.State) error {
		if req.Interval != nil {
			st.Settings.Interval = *req.Interval
		}
		if req.Timeout != nil {
			st.Settings.Timeout = *req.Timeout
		}
		if req.NotifyChanges != nil {
			st.Settings.NotifyChanges = *req.NotifyChanges
		}
		if err := st.Settings.Validate(); err != nil {
			return badRequest{err}
		}
		settings = st.Settings
		return nil
	}) {
		return
	}
	s.events.publish("settings", settings)
	s.monitor.Wake()
	writeJSON(w, settings)
}
