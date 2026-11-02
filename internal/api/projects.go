package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/floriandejonckheere/dropcatch/internal/store"
)

const (
	maxToken = 200
	// maxOrder bounds the servers one order creates; Hetzner's default
	// limits are lower still.
	maxOrder = 20
)

// serverName is a hostname label, followed by "-1", "-2", … when ordered.
var serverName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,50}[a-z0-9])?$`)

// projectView is a project without its token, which is never sent back.
type projectView struct {
	ID    string    `json:"id"`
	Name  string    `json:"name"`
	Token string    `json:"token"` // its last characters, to tell tokens apart
	Added time.Time `json:"added"`
}

func projectViews(ps []store.Project) []projectView {
	out := []projectView{}
	for _, p := range ps {
		out = append(out, projectView{ID: p.ID, Name: p.Name, Token: "…" + p.Token[max(0, len(p.Token)-4):], Added: p.Added})
	}
	return out
}

func (s *Server) publishProjects() {
	s.events.publish("projects", projectViews(s.store.Snapshot().Projects))
}

// checkToken checks that a project token can create servers.
func (s *Server) checkToken(ctx context.Context, token string) error {
	if token == "" || len(token) > maxToken || strings.ContainsAny(token, " \t\r\n") {
		return badRequest{errors.New("enter the project's API token")}
	}
	ctx, cancel := context.WithTimeout(ctx, ovhQuery)
	defer cancel()
	if err := s.hetzner.CheckProjectToken(ctx, token); err != nil {
		return badRequest{err}
	}
	return nil
}

func validProjectName(name string) error {
	if name == "" || len(name) > maxName {
		return badRequest{errors.New("enter a name of at most 100 characters")}
	}
	return nil
}

func (s *Server) handleProjectAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string `json:"name"`
		Token string `json:"token"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Name, req.Token = strings.TrimSpace(req.Name), strings.TrimSpace(req.Token)
	err := validProjectName(req.Name)
	if err == nil {
		err = s.checkToken(r.Context(), req.Token)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var p store.Project
	if !s.update(w, func(st *store.State) error {
		p = *st.AddProject(store.Project{Name: req.Name, Token: req.Token}, time.Now().UTC())
		return nil
	}) {
		return
	}
	s.publishProjects()
	writeJSON(w, projectViews([]store.Project{p})[0])
}

func (s *Server) handleProjectEdit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  *string `json:"name"`
		Token *string `json:"token"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Name != nil {
		*req.Name = strings.TrimSpace(*req.Name)
		if err := validProjectName(*req.Name); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if req.Token != nil {
		*req.Token = strings.TrimSpace(*req.Token)
		if err := s.checkToken(r.Context(), *req.Token); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if !s.update(w, func(st *store.State) error {
		p := st.Project(r.PathValue("id"))
		if p == nil {
			return store.ErrNotFound
		}
		if req.Name != nil {
			p.Name = *req.Name
		}
		if req.Token != nil {
			p.Token = *req.Token
		}
		return nil
	}) {
		return
	}
	s.publishProjects()
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleProjectRemove(w http.ResponseWriter, r *http.Request) {
	if !s.update(w, func(st *store.State) error { return st.RemoveProject(r.PathValue("id")) }) {
		return
	}
	s.publishProjects()
	writeJSON(w, map[string]bool{"ok": true})
}

// handleProjectOptions lists the SSH keys of a project and the images for
// an architecture, for the order form.
func (s *Server) handleProjectOptions(w http.ResponseWriter, r *http.Request) {
	p := s.store.Snapshot().Project(r.PathValue("id"))
	if p == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	arch := r.URL.Query().Get("architecture")
	if arch != "x86" && arch != "arm" {
		writeError(w, http.StatusBadRequest, "unknown architecture")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), ovhQuery)
	defer cancel()
	keys, err := s.hetzner.SSHKeys(ctx, p.Token)
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not load the SSH keys: "+err.Error())
		return
	}
	images, err := s.hetzner.Images(ctx, p.Token, arch)
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not load the images: "+err.Error())
		return
	}
	writeJSON(w, map[string]any{"sshKeys": keys, "images": images})
}

// orderRequest is the order of a Hetzner watch, as the form sends it.
type orderRequest struct {
	Active   bool            `json:"active"`
	Project  string          `json:"project"`
	Location string          `json:"location"`
	Quantity int             `json:"quantity"`
	Image    store.NamedID   `json:"image"`
	SSHKeys  []store.NamedID `json:"sshKeys"`
	Name     string          `json:"name"`
}

// decodeOrder reads the order of a request: nil when the request has
// none, and a nil order when it removes it ("order": null).
func decodeOrder(raw json.RawMessage) (*orderRequest, bool, error) {
	if raw == nil {
		return nil, false, nil
	}
	if string(raw) == "null" {
		return nil, true, nil
	}
	var o orderRequest
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, false, badRequest{errors.New("invalid order: " + err.Error())}
	}
	return &o, true, nil
}

// setOrder validates an order and applies it to the watch. Starting an
// order (again) begins a new round with no servers; changing a running
// order keeps the servers it created.
func setOrder(st *store.State, v *store.VPS, req *orderRequest) error {
	if req == nil {
		v.Order = nil
		return nil
	}
	if v.Provider != store.Hetzner {
		return badRequest{errors.New("servers can only be ordered from Hetzner Cloud")}
	}
	req.Name = strings.ToLower(strings.TrimSpace(req.Name))
	if req.Name == "" {
		req.Name = v.Plan
	}
	switch {
	case st.Project(req.Project) == nil:
		return badRequest{errors.New("choose the Hetzner project to order in")}
	case req.Location != "" && !datacenterCode.MatchString(req.Location):
		return badRequest{errors.New("invalid location")}
	case req.Quantity < 1 || req.Quantity > maxOrder:
		return badRequest{errors.New("order between 1 and 20 servers")}
	case req.Image.ID <= 0:
		return badRequest{errors.New("choose an image")}
	case len(req.SSHKeys) == 0:
		// Without one, Hetzner sets a root password that only the
		// creating request gets to see.
		return badRequest{errors.New("choose at least one SSH key")}
	case len(req.SSHKeys) > 50:
		return badRequest{errors.New("too many SSH keys")}
	case !serverName.MatchString(req.Name):
		return badRequest{errors.New("the server name may only have lowercase letters, digits and hyphens")}
	}
	o := &store.Order{
		Active: req.Active, Project: req.Project, Location: req.Location, Quantity: req.Quantity,
		Image: req.Image, SSHKeys: req.SSHKeys, Name: req.Name,
	}
	if prev := v.Order; prev != nil && (prev.Active || !req.Active) {
		o.Batch, o.Servers, o.Error, o.Tried = prev.Batch, prev.Servers, prev.Error, prev.Tried
		if o.Active && o.Done() {
			o.Active = false
		}
	} else if o.Active {
		o.Batch = store.NewID()
	}
	v.Order = o
	return nil
}
