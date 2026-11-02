package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/floriandejonckheere/dropcatch/internal/hetzner"
	"github.com/floriandejonckheere/dropcatch/internal/monitor"
	"github.com/floriandejonckheere/dropcatch/internal/ovh"
	"github.com/floriandejonckheere/dropcatch/internal/store"
)

var (
	// Plan codes are case-sensitive: Local Zone plans end in ".LZ".
	planCode       = regexp.MustCompile(`^[a-z0-9][a-zA-Z0-9._-]{1,63}$`)
	serverType     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,39}$`)
	datacenterCode = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,39}$`)
)

// ovhQuery bounds the OVH requests made while the user fills in the form.
const ovhQuery = 20 * time.Second

func (s *Server) subsidiary(w http.ResponseWriter, r *http.Request) (string, bool) {
	sub := r.URL.Query().Get("subsidiary")
	if !ovh.ValidSubsidiary(sub) {
		writeError(w, http.StatusBadRequest, "unknown OVH subsidiary")
		return "", false
	}
	return sub, true
}

// handleOVHPlans lists the VPS plans of a subsidiary for the form.
func (s *Server) handleOVHPlans(w http.ResponseWriter, r *http.Request) {
	sub, ok := s.subsidiary(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), ovhQuery)
	defer cancel()
	cat, err := s.ovh.Plans(ctx, sub)
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not load the OVH catalog: "+err.Error())
		return
	}
	writeJSON(w, cat)
}

// handleOVHStock lists the datacenters of a plan with their current stock,
// so that the form can offer them.
func (s *Server) handleOVHStock(w http.ResponseWriter, r *http.Request) {
	sub, ok := s.subsidiary(w, r)
	if !ok {
		return
	}
	plan := r.URL.Query().Get("plan")
	if !planCode.MatchString(plan) {
		writeError(w, http.StatusBadRequest, "invalid plan code")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), ovhQuery)
	defer cancel()
	dcs, err := s.ovh.Stock(ctx, sub, plan)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, map[string]any{"datacenters": dcs})
}

// hetznerToken returns the token that Hetzner's server types are read
// with, or answers that a project is needed.
func (s *Server) hetznerToken(w http.ResponseWriter) (string, bool) {
	token := s.store.Snapshot().HetznerToken()
	if token == "" {
		writeError(w, http.StatusBadRequest, hetzner.ErrNoToken.Error())
	}
	return token, token != ""
}

// handleHetznerPlans lists the Hetzner Cloud server types for the form.
func (s *Server) handleHetznerPlans(w http.ResponseWriter, r *http.Request) {
	token, ok := s.hetznerToken(w)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), ovhQuery)
	defer cancel()
	cat, err := s.hetzner.Plans(ctx, token)
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not load the Hetzner server types: "+err.Error())
		return
	}
	writeJSON(w, cat)
}

// handleHetznerStock lists the locations of a server type with its
// current availability, so that the form can offer them.
func (s *Server) handleHetznerStock(w http.ResponseWriter, r *http.Request) {
	plan := r.URL.Query().Get("plan")
	if !serverType.MatchString(plan) {
		writeError(w, http.StatusBadRequest, "invalid server type")
		return
	}
	token, ok := s.hetznerToken(w)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), ovhQuery)
	defer cancel()
	locs, err := s.hetzner.Stock(ctx, token, plan)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, map[string]any{"datacenters": locs})
}

// cleanDatacenters validates and sorts datacenter codes.
func cleanDatacenters(codes []string) ([]string, error) {
	out := []string{}
	for _, c := range codes {
		c = strings.ToLower(strings.TrimSpace(c))
		if !datacenterCode.MatchString(c) {
			return nil, badRequest{errors.New("invalid datacenter code: " + c)}
		}
		if !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	slices.Sort(out)
	return out, nil
}

func (s *Server) handleVPSAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider    string          `json:"provider"`
		Subsidiary  string          `json:"subsidiary"`
		Plan        string          `json:"plan"`
		PlanName    string          `json:"planName"`
		Datacenters []string        `json:"datacenters"`
		Note        string          `json:"note"`
		Interval    int             `json:"interval"`
		Muted       bool            `json:"muted"`
		Order       json.RawMessage `json:"order"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Plan = strings.TrimSpace(req.Plan)
	req.PlanName = strings.TrimSpace(req.PlanName)
	switch req.Provider {
	case "", store.OVH:
		req.Provider = store.OVH
		if !ovh.ValidSubsidiary(req.Subsidiary) {
			writeError(w, http.StatusBadRequest, "unknown OVH subsidiary")
			return
		}
		if !planCode.MatchString(req.Plan) {
			writeError(w, http.StatusBadRequest, "enter a plan code, such as vps-2025-model1")
			return
		}
	case store.Hetzner:
		if !serverType.MatchString(req.Plan) {
			writeError(w, http.StatusBadRequest, "enter a server type, such as cx23")
			return
		}
		// Hetzner has no country shops.
		req.Subsidiary = ""
	default:
		writeError(w, http.StatusBadRequest, "unknown provider")
		return
	}
	switch {
	case len(req.PlanName) > maxName || len(req.Note) > maxNote:
		writeError(w, http.StatusBadRequest, "the plan name or note is too long")
		return
	}
	if err := ownInterval(req.Interval, store.ValidateVPSInterval); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.PlanName == "" {
		req.PlanName = req.Plan
	}
	dcs, err := cleanDatacenters(req.Datacenters)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	order, _, err := decodeOrder(req.Order)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var v store.VPS
	if !s.update(w, func(st *store.State) error {
		if req.Provider == store.Hetzner && st.HetznerToken() == "" {
			return badRequest{hetzner.ErrNoToken}
		}
		p, err := st.AddVPS(store.VPS{
			Provider: req.Provider, Subsidiary: req.Subsidiary, Plan: req.Plan, PlanName: req.PlanName,
			Datacenters: dcs, Note: strings.TrimSpace(req.Note), Interval: req.Interval, Muted: req.Muted,
		}, time.Now().UTC())
		if err != nil {
			return err
		}
		if order != nil {
			if err := setOrder(st, p, order); err != nil {
				return err
			}
		}
		v = *p
		return nil
	}) {
		return
	}
	s.events.publish("vps", v)
	s.monitor.Check(monitor.VPSKey(v.ID))
	writeJSON(w, v)
}

func (s *Server) handleVPSEdit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Datacenters *[]string       `json:"datacenters"`
		Note        *string         `json:"note"`
		Paused      *bool           `json:"paused"`
		Muted       *bool           `json:"muted"`
		Interval    *int            `json:"interval"` // 0 for the setting
		Order       json.RawMessage `json:"order"`    // null removes it
	}
	if !decode(w, r, &req) {
		return
	}
	order, setsOrder, err := decodeOrder(req.Order)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var v store.VPS
	if !s.update(w, func(st *store.State) error {
		p := st.VPSWatch(r.PathValue("id"))
		if p == nil {
			return store.ErrNotFound
		}
		if req.Datacenters != nil {
			dcs, err := cleanDatacenters(*req.Datacenters)
			if err != nil {
				return err
			}
			p.Datacenters = dcs
			// Recompute the status for the new selection, without
			// notifying: the stock itself did not change.
			if p.Status != store.Unknown {
				p.SetStock(p.Stock, time.Now().UTC())
			}
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
		if req.Muted != nil {
			p.Muted = *req.Muted
		}
		if req.Interval != nil {
			if err := ownInterval(*req.Interval, store.ValidateVPSInterval); err != nil {
				return err
			}
			p.Interval = *req.Interval
		}
		if setsOrder {
			if err := setOrder(st, p, order); err != nil {
				return err
			}
		}
		v = *p
		return nil
	}) {
		return
	}
	s.events.publish("vps", v)
	if setsOrder && v.Order != nil && v.Order.Active && !v.Paused {
		// Order at once when it is in stock now.
		s.monitor.Check(monitor.VPSKey(v.ID))
	} else {
		s.monitor.Wake()
	}
	writeJSON(w, v)
}

func (s *Server) handleVPSRemove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.update(w, func(st *store.State) error { return st.RemoveVPS(id) }) {
		return
	}
	s.events.publish("vpsRemoved", id)
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleVPSCheck(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.store.Snapshot().VPSWatch(id) == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	s.monitor.Check(monitor.VPSKey(id))
	writeJSONStatus(w, http.StatusAccepted, map[string]bool{"ok": true})
}
