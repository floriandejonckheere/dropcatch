package hetzner

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newProjectAPI serves a project for the tokens "rw" (Read & Write) and
// "ro" (read-only).
func newProjectAPI(t *testing.T) (*Client, *[]map[string]any) {
	var created []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer rw" && auth != "Bearer ro" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"unable to authenticate"}}`))
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/ssh_keys":
			_, _ = w.Write([]byte(`{"ssh_keys":[{"id":2,"name":"work","fingerprint":"x"},{"id":1,"name":"laptop"}],"meta":{"pagination":{"next_page":null}}}`))
		case "GET /v1/images":
			if q := r.URL.Query(); q.Get("architecture") != "arm" || q.Get("type") != "system" {
				t.Errorf("images query %v", q)
			}
			_, _ = w.Write([]byte(`{"images":[
				{"id":11,"name":"ubuntu-22.04","description":"Ubuntu 22.04"},{"id":12,"name":"ubuntu-24.04","description":"Ubuntu 24.04"},
				{"id":13,"name":"debian-13","description":"Debian 13"},{"id":14,"name":null,"description":"snapshot"}],"meta":{"pagination":{"next_page":null}}}`))
		case "GET /v1/servers":
			if r.URL.Query().Get("label_selector") != "dropcatch=w1" {
				t.Errorf("servers query %v", r.URL.Query())
			}
			_, _ = w.Write([]byte(`{"servers":[{"id":5,"name":"cax11-1","labels":{"dropcatch":"w1"},"datacenter":{"location":{"name":"fsn1"}}}],"meta":{"pagination":{"next_page":null}}}`))
		case "POST /v1/servers":
			if auth == "Bearer ro" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"code":"token_readonly","message":"The token is only allowed to perform GET requests"}}`))
				return
			}
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req["name"] == nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"code":"invalid_input","message":"invalid input in field 'name'"}}`))
				return
			}
			if req["location"] == "hel1" {
				w.WriteHeader(http.StatusPreconditionFailed)
				_, _ = w.Write([]byte(`{"error":{"code":"resource_unavailable","message":"server type cax11 is unavailable in hel1"}}`))
				return
			}
			created = append(created, req)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"server":{"id":6,"name":"cax11-2","labels":{"dropcatch":"w1"},"datacenter":{"location":{"name":"fsn1"}}},"root_password":null}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c := New("test", "")
	c.BaseURL = srv.URL + "/v1"
	return c, &created
}

func TestProject(t *testing.T) {
	c, created := newProjectAPI(t)
	ctx := t.Context()

	if err := c.CheckProjectToken(ctx, "rw"); err != nil {
		t.Fatalf("read & write token: %v", err)
	}
	if err := c.CheckProjectToken(ctx, "ro"); err == nil || err.Error() != "the token is read-only: create one with the Read & Write permission" {
		t.Fatalf("read-only token: %v", err)
	}
	if err := c.CheckProjectToken(ctx, "nope"); err == nil || err.Error() != "Hetzner: the API token was refused (401)" {
		t.Fatalf("unknown token: %v", err)
	}

	keys, err := c.SSHKeys(ctx, "rw")
	if err != nil || len(keys) != 2 || keys[0].Name != "laptop" {
		t.Fatalf("ssh keys %+v, %v", keys, err)
	}
	images, err := c.Images(ctx, "rw", "arm")
	if err != nil || len(images) != 3 || images[0].Name != "debian-13" || images[1].Name != "ubuntu-24.04" || images[2].ID != 11 {
		t.Fatalf("images %+v, %v", images, err)
	}
	servers, err := c.Servers(ctx, "rw", "dropcatch=w1")
	if err != nil || len(servers) != 1 || servers[0].Location != "fsn1" || servers[0].Labels["dropcatch"] != "w1" {
		t.Fatalf("servers %+v, %v", servers, err)
	}

	s, err := c.Create(ctx, "rw", CreateServer{Name: "cax11-2", ServerType: "cax11", Location: "fsn1", Image: 13, SSHKeys: []int64{1}, Labels: map[string]string{"dropcatch": "w1"}})
	if err != nil || s.ID != 6 || s.Location != "fsn1" {
		t.Fatalf("created %+v, %v", s, err)
	}
	req := (*created)[0]
	if req["server_type"] != "cax11" || req["image"] != 13.0 || req["start_after_create"] != true || req["ssh_keys"].([]any)[0] != 1.0 {
		t.Fatalf("request %v", req)
	}

	_, err = c.Create(ctx, "rw", CreateServer{Name: "cax11-3", Location: "hel1"})
	if !Unavailable(err) || Readonly(err) {
		t.Fatalf("out of stock: %v", err)
	}
	_, err = c.Create(ctx, "ro", CreateServer{Name: "cax11-3"})
	if Unavailable(err) || !Readonly(err) {
		t.Fatalf("read-only: %v", err)
	}
}
