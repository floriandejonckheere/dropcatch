package hetzner

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// Ordering servers needs a token of the project they are created in, with
// the Read & Write permission. A token belongs to one project, so the
// project is chosen by its token.

// SSHKey is an SSH key stored in a project, added to the servers created
// there.
type SSHKey struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Image is an operating system servers can be created with. System images
// are built per architecture, each with its own ID.
type Image struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"` // ubuntu-24.04
	Description string `json:"description"`
}

// Server is a server created in a project.
type Server struct {
	ID       int64             `json:"id"`
	Name     string            `json:"name"`
	Location string            `json:"location"`
	Labels   map[string]string `json:"labels"`
}

// CreateServer describes a server to create.
type CreateServer struct {
	Name       string
	ServerType string
	Location   string
	Image      int64
	SSHKeys    []int64
	Labels     map[string]string
}

// Readonly reports whether err is the answer to a token that may only
// read.
func Readonly(err error) bool {
	e, ok := errors.AsType[*APIError](err)
	return ok && (e.Code == "token_readonly" || e.Code == "forbidden")
}

// Unavailable reports whether err means that the server could not be
// created for now, because the type is out of stock in the location or
// Hetzner is busy, so that trying again at the next restock may work.
// Other errors (a bad token, a missing image, a resource limit) will not
// go away by themselves.
func Unavailable(err error) bool {
	e, ok := errors.AsType[*APIError](err)
	if !ok {
		return true // no answer: a network error or timeout
	}
	switch e.Code {
	case "resource_unavailable", "placement_error", "rate_limit_exceeded", "locked", "conflict",
		"maintenance", "service_error", "server_error", "bad_gateway", "timeout", "unavailable":
		return true
	}
	return e.Status == http.StatusTooManyRequests || e.Status >= 500
}

// CheckProjectToken checks that token can read the project and create
// servers in it. Creating a server needs a name, type and image, so an
// empty request creates nothing: Hetzner refuses it as invalid when the
// token may write, and as read-only before looking at it when it may not.
func (c *Client) CheckProjectToken(ctx context.Context, token string) error {
	if _, err := c.SSHKeys(ctx, token); err != nil {
		return projectError(err)
	}
	err := c.do(ctx, token, http.MethodPost, "/servers", nil, struct{}{}, nil)
	switch e, ok := errors.AsType[*APIError](err); {
	case err == nil:
		return errors.New("Hetzner accepted an empty server")
	case Readonly(err):
		return errors.New("the token is read-only: create one with the Read & Write permission")
	case ok && (e.Code == "invalid_input" || e.Code == "json_error" || e.Status == http.StatusBadRequest || e.Status == http.StatusUnprocessableEntity):
		return nil
	default:
		return projectError(err)
	}
}

func projectError(err error) error {
	if e, ok := errors.AsType[*APIError](err); ok && e.Status == http.StatusUnauthorized && e.Code != "token_readonly" {
		return &APIError{Status: e.Status, Code: e.Code, Message: "the API token was refused"}
	}
	return err
}

// SSHKeys lists the SSH keys of the token's project.
func (c *Client) SSHKeys(ctx context.Context, token string) ([]SSHKey, error) {
	var out []SSHKey
	for page := 1; page > 0; {
		var res struct {
			SSHKeys []SSHKey `json:"ssh_keys"`
			Meta    meta     `json:"meta"`
		}
		if err := c.do(ctx, token, http.MethodGet, "/ssh_keys", url.Values{"per_page": {"50"}, "page": {strconv.Itoa(page)}}, nil, &res); err != nil {
			return nil, projectError(err)
		}
		out = append(out, res.SSHKeys...)
		page = res.Meta.Pagination.NextPage
	}
	slices.SortFunc(out, func(a, b SSHKey) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// Images lists the operating systems for an architecture (x86 or arm),
// newest version first within each system.
func (c *Client) Images(ctx context.Context, token, arch string) ([]Image, error) {
	var out []Image
	for page := 1; page > 0; {
		var res struct {
			Images []struct {
				ID          int64   `json:"id"`
				Name        *string `json:"name"`
				Description string  `json:"description"`
				OSFlavor    string  `json:"os_flavor"`
			} `json:"images"`
			Meta meta `json:"meta"`
		}
		q := url.Values{"type": {"system"}, "status": {"available"}, "architecture": {arch}, "per_page": {"50"}, "page": {strconv.Itoa(page)}}
		if err := c.do(ctx, token, http.MethodGet, "/images", q, nil, &res); err != nil {
			return nil, projectError(err)
		}
		for _, i := range res.Images {
			if i.Name != nil {
				out = append(out, Image{ID: i.ID, Name: *i.Name, Description: cmp.Or(i.Description, *i.Name)})
			}
		}
		page = res.Meta.Pagination.NextPage
	}
	slices.SortFunc(out, func(a, b Image) int {
		fa, va, _ := strings.Cut(a.Name, "-")
		fb, vb, _ := strings.Cut(b.Name, "-")
		return cmp.Or(strings.Compare(fa, fb), compareVersions(vb, va))
	})
	return out, nil
}

// compareVersions compares dotted versions numerically: 24.04 after 9.
func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range min(len(pa), len(pb)) {
		na, ea := strconv.Atoi(pa[i])
		nb, eb := strconv.Atoi(pb[i])
		if ea != nil || eb != nil {
			if c := strings.Compare(pa[i], pb[i]); c != 0 {
				return c
			}
			continue
		}
		if c := cmp.Compare(na, nb); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(pa), len(pb))
}

// Servers lists the servers of the token's project, all of them or those
// that match a label selector, such as "dropcatch=abc".
func (c *Client) Servers(ctx context.Context, token, selector string) ([]Server, error) {
	var out []Server
	for page := 1; page > 0; {
		var res struct {
			Servers []rawServer `json:"servers"`
			Meta    meta        `json:"meta"`
		}
		q := url.Values{"per_page": {"50"}, "page": {strconv.Itoa(page)}}
		if selector != "" {
			q.Set("label_selector", selector)
		}
		if err := c.do(ctx, token, http.MethodGet, "/servers", q, nil, &res); err != nil {
			return nil, projectError(err)
		}
		for _, s := range res.Servers {
			out = append(out, s.server())
		}
		page = res.Meta.Pagination.NextPage
	}
	return out, nil
}

// Create creates and starts a server.
func (c *Client) Create(ctx context.Context, token string, s CreateServer) (Server, error) {
	req := map[string]any{
		"name": s.Name, "server_type": s.ServerType, "location": s.Location, "image": s.Image,
		"ssh_keys": s.SSHKeys, "labels": s.Labels, "start_after_create": true,
	}
	var res struct {
		Server rawServer `json:"server"`
	}
	if err := c.do(ctx, token, http.MethodPost, "/servers", nil, req, &res); err != nil {
		return Server{}, projectError(err)
	}
	return res.Server.server(), nil
}

type meta struct {
	Pagination struct {
		NextPage int `json:"next_page"`
	} `json:"pagination"`
}

type rawServer struct {
	ID         int64             `json:"id"`
	Name       string            `json:"name"`
	Labels     map[string]string `json:"labels"`
	Datacenter struct {
		Location struct {
			Name string `json:"name"`
		} `json:"location"`
	} `json:"datacenter"`
}

func (s rawServer) server() Server {
	return Server{ID: s.ID, Name: s.Name, Location: s.Datacenter.Location.Name, Labels: s.Labels}
}
