package api

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/floriandejonckheere/dropcatch/internal/monitor"
	"github.com/floriandejonckheere/dropcatch/internal/ovh"
	"github.com/floriandejonckheere/dropcatch/internal/store"
)

var (
	planCode       = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,63}$`)
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
		Subsidiary  string   `json:"subsidiary"`
		Plan        string   `json:"plan"`
		PlanName    string   `json:"planName"`
		Datacenters []string `json:"datacenters"`
		Note        string   `json:"note"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Plan = strings.ToLower(strings.TrimSpace(req.Plan))
	req.PlanName = strings.TrimSpace(req.PlanName)
	switch {
	case !ovh.ValidSubsidiary(req.Subsidiary):
		writeError(w, http.StatusBadRequest, "unknown OVH subsidiary")
		return
	case !planCode.MatchString(req.Plan):
		writeError(w, http.StatusBadRequest, "enter a plan code, such as vps-2025-model1")
		return
	case len(req.PlanName) > maxName || len(req.Note) > maxNote:
		writeError(w, http.StatusBadRequest, "the plan name or note is too long")
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
	var v store.VPS
	if !s.update(w, func(st *store.State) error {
		p, err := st.AddVPS(store.VPS{
			Subsidiary: req.Subsidiary, Plan: req.Plan, PlanName: req.PlanName,
			Datacenters: dcs, Note: strings.TrimSpace(req.Note),
		}, time.Now().UTC())
		if err != nil {
			return err
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
		Datacenters *[]string `json:"datacenters"`
		Note        *string   `json:"note"`
		Paused      *bool     `json:"paused"`
	}
	if !decode(w, r, &req) {
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
		v = *p
		return nil
	}) {
		return
	}
	s.events.publish("vps", v)
	s.monitor.Wake()
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
