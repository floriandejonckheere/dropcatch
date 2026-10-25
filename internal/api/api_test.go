package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/floriandejonckheere/dropcatch/internal/config"
	"github.com/floriandejonckheere/dropcatch/internal/lookup"
	"github.com/floriandejonckheere/dropcatch/internal/monitor"
	"github.com/floriandejonckheere/dropcatch/internal/notify"
	"github.com/floriandejonckheere/dropcatch/internal/ovh"
	"github.com/floriandejonckheere/dropcatch/internal/store"
)

type noLookup struct{}

func (noLookup) Lookup(context.Context, string) (lookup.Result, error) {
	return lookup.Result{Status: lookup.Registered}, nil
}

type fakeOVH struct{}

func (fakeOVH) Plans(_ context.Context, sub string) (ovh.Catalog, error) {
	return ovh.Catalog{Currency: "EUR", Plans: []ovh.Plan{{Code: "vps-2025-model1", Name: "VPS-1", Cores: 4, Memory: 8, Storage: 75, Price: 4.2}}}, nil
}

func (fakeOVH) Stock(_ context.Context, sub, plan string) ([]ovh.Datacenter, error) {
	if plan != "vps-2025-model1" {
		return nil, errors.New("no such plan")
	}
	return []ovh.Datacenter{{Code: "eu-west-gra", Name: "Gravelines, France", Region: ovh.Europe, Available: true}}, nil
}

type testServer struct {
	t      *testing.T
	h      http.Handler
	store  *store.Store
	cookie *http.Cookie
}

func newServer(t *testing.T, password string) *testServer {
	cfg := &config.Config{DataDir: t.TempDir(), BasePath: "/", Password: password}
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	apprise := &notify.Apprise{}
	o := &fakeOVH{}
	m := monitor.New(st, noLookup{}, o, apprise)
	ui := fstest.MapFS{"index.html": {Data: []byte("<html>dropcatch</html>")}}
	s, err := New(cfg, st, m, o, apprise, ui)
	if err != nil {
		t.Fatal(err)
	}
	return &testServer{t: t, h: s.Handler(), store: st}
}

// do sends a JSON request and decodes the JSON response into out.
func (ts *testServer) do(method, path string, body any, out any) int {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	r := httptest.NewRequest(method, path, &buf)
	if ts.cookie != nil {
		r.AddCookie(ts.cookie)
	}
	w := httptest.NewRecorder()
	ts.h.ServeHTTP(w, r)
	for _, c := range w.Result().Cookies() {
		ts.cookie = c
	}
	if out != nil {
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			ts.t.Fatalf("%s %s: %v: %s", method, path, err, w.Body)
		}
	}
	return w.Code
}

func TestAuth(t *testing.T) {
	ts := newServer(t, "hunter2")
	if code := ts.do("GET", "/api/state", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("state without login: %d", code)
	}
	var cfg struct {
		Auth struct{ Required, Authenticated bool }
	}
	if ts.do("GET", "/api/config", nil, &cfg); !cfg.Auth.Required || cfg.Auth.Authenticated {
		t.Fatalf("config = %+v", cfg)
	}
	if code := ts.do("POST", "/api/login", map[string]string{"username": "", "password": "wrong"}, nil); code != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", code)
	}
	if code := ts.do("POST", "/api/login", map[string]string{"username": "", "password": "hunter2"}, nil); code != http.StatusOK {
		t.Fatalf("login: %d", code)
	}
	if code := ts.do("GET", "/api/state", nil, nil); code != http.StatusOK {
		t.Fatalf("state after login: %d", code)
	}
	ts.do("POST", "/api/logout", map[string]string{}, nil)
	if code := ts.do("GET", "/api/state", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("state after logout: %d", code)
	}

	// The UI shell is public.
	r := httptest.NewRequest("GET", "/some/route", nil)
	w := httptest.NewRecorder()
	ts.h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "dropcatch") || w.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("UI: %d %q", w.Code, w.Body)
	}
}

func TestDomains(t *testing.T) {
	ts := newServer(t, "")
	var add struct {
		Added  []domainView
		Failed []struct{ Input, Error string }
	}
	code := ts.do("POST", "/api/domains", map[string]string{"names": "Example.com, www.bücher.de\nnot_a.com\nexample.com", "note": " for the shop "}, &add)
	if code != http.StatusOK || len(add.Added) != 2 || len(add.Failed) != 2 {
		t.Fatalf("add: %d %+v", code, add)
	}
	if d := add.Added[1]; d.Name != "xn--bcher-kva.de" || d.Display != "bücher.de" || d.Note != "for the shop" || d.Status != store.Unknown {
		t.Fatalf("added %+v", d)
	}
	if add.Failed[1].Input != "example.com" || add.Failed[1].Error != "already monitored" {
		t.Fatalf("failed = %+v", add.Failed)
	}
	if code := ts.do("POST", "/api/domains", map[string]string{"names": " "}, nil); code != http.StatusBadRequest {
		t.Fatalf("adding nothing: %d", code)
	}

	var d domainView
	if code := ts.do("PATCH", "/api/domains/example.com", map[string]any{"paused": true, "note": "later"}, &d); code != http.StatusOK || !d.Paused || d.Note != "later" {
		t.Fatalf("edit: %d %+v", code, d)
	}
	if code := ts.do("PATCH", "/api/domains/missing.com", map[string]any{"paused": true}, nil); code != http.StatusNotFound {
		t.Fatalf("edit missing: %d", code)
	}
	if code := ts.do("POST", "/api/domains/example.com/check", nil, nil); code != http.StatusAccepted {
		t.Fatalf("check: %d", code)
	}
	if code := ts.do("DELETE", "/api/domains/example.com", nil, nil); code != http.StatusOK {
		t.Fatalf("remove: %d", code)
	}
	var st struct {
		Domains  []domainView
		Checking []string
	}
	ts.do("GET", "/api/state", nil, &st)
	if len(st.Domains) != 1 || st.Domains[0].Name != "xn--bcher-kva.de" {
		t.Fatalf("state = %+v", st)
	}
}

func TestTargetsAndSettings(t *testing.T) {
	ts := newServer(t, "")
	var tg store.Target
	if code := ts.do("POST", "/api/targets", map[string]any{"name": "Phone", "url": "ntfy://topic", "enabled": true}, &tg); code != http.StatusOK || tg.ID == "" {
		t.Fatalf("add target: %d %+v", code, tg)
	}
	for _, bad := range []map[string]any{{"name": "", "url": "ntfy://x"}, {"name": "x", "url": "not a url"}} {
		if code := ts.do("POST", "/api/targets", bad, nil); code != http.StatusBadRequest {
			t.Errorf("add %v: %d", bad, code)
		}
	}
	if code := ts.do("PATCH", "/api/targets/"+tg.ID, map[string]any{"enabled": false}, &tg); code != http.StatusOK || tg.Enabled {
		t.Fatalf("disable target: %d %+v", code, tg)
	}
	// Without Apprise installed, the test reports why nothing was sent.
	var res struct{ Error string }
	if code := ts.do("POST", "/api/targets/"+tg.ID+"/test", nil, &res); code != http.StatusBadGateway || !strings.Contains(res.Error, "not installed") {
		t.Fatalf("test target: %d %+v", code, res)
	}
	if code := ts.do("DELETE", "/api/targets/"+tg.ID, nil, nil); code != http.StatusOK {
		t.Fatalf("remove target: %d", code)
	}
	if code := ts.do("DELETE", "/api/targets/"+tg.ID, nil, nil); code != http.StatusNotFound {
		t.Fatalf("remove target twice: %d", code)
	}

	var settings store.Settings
	if code := ts.do("PATCH", "/api/settings", map[string]any{"interval": 15, "notifyChanges": false}, &settings); code != http.StatusOK ||
		settings.Interval != 15 || settings.NotifyChanges || settings.Timeout != store.DefaultSettings().Timeout {
		t.Fatalf("settings: %d %+v", code, settings)
	}
	if code := ts.do("PATCH", "/api/settings", map[string]any{"stockInterval": 5}, &settings); code != http.StatusOK || settings.StockInterval != 5 {
		t.Fatalf("stock interval: %d %+v", code, settings)
	}
	if code := ts.do("PATCH", "/api/settings", map[string]any{"interval": 1}, nil); code != http.StatusBadRequest {
		t.Fatalf("too short interval: %d", code)
	}
	if code := ts.do("PATCH", "/api/settings", map[string]any{"stockInterval": 0}, nil); code != http.StatusBadRequest {
		t.Fatalf("too short stock interval: %d", code)
	}
	if ts.store.Snapshot().Settings.Interval != 15 {
		t.Fatal("rejected settings were saved")
	}
}

func TestCrossOrigin(t *testing.T) {
	ts := newServer(t, "")
	r := httptest.NewRequest("POST", "/api/check", nil)
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	ts.h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin POST: %d", w.Code)
	}
}

func TestVPS(t *testing.T) {
	ts := newServer(t, "")
	var cat ovh.Catalog
	if code := ts.do("GET", "/api/ovh/plans?subsidiary=FR", nil, &cat); code != http.StatusOK || len(cat.Plans) != 1 {
		t.Fatalf("plans: %d %+v", code, cat)
	}
	if code := ts.do("GET", "/api/ovh/plans?subsidiary=XX", nil, nil); code != http.StatusBadRequest {
		t.Fatalf("plans of an unknown subsidiary: %d", code)
	}
	var stock struct{ Datacenters []ovh.Datacenter }
	if code := ts.do("GET", "/api/ovh/stock?subsidiary=FR&plan=vps-2025-model1", nil, &stock); code != http.StatusOK || len(stock.Datacenters) != 1 {
		t.Fatalf("stock: %d %+v", code, stock)
	}
	if code := ts.do("GET", "/api/ovh/stock?subsidiary=FR&plan=nope", nil, nil); code != http.StatusBadGateway {
		t.Fatalf("stock of an unknown plan: %d", code)
	}

	var v store.VPS
	add := map[string]any{"subsidiary": "FR", "plan": "vps-2025-model1", "planName": "VPS-1", "datacenters": []string{"eu-west-sbg", "EU-WEST-GRA", "eu-west-sbg"}}
	if code := ts.do("POST", "/api/vps", add, &v); code != http.StatusOK || v.ID == "" || v.Status != store.Unknown ||
		strings.Join(v.Datacenters, ",") != "eu-west-gra,eu-west-sbg" {
		t.Fatalf("add: %d %+v", code, v)
	}
	if code := ts.do("POST", "/api/vps", add, nil); code != http.StatusConflict {
		t.Fatalf("adding the same watch twice: %d", code)
	}
	for _, bad := range []map[string]any{
		{"subsidiary": "XX", "plan": "vps-2025-model1"},
		{"subsidiary": "FR", "plan": ""},
		{"subsidiary": "FR", "plan": "vps-1", "datacenters": []string{"bad code!"}},
	} {
		if code := ts.do("POST", "/api/vps", bad, nil); code != http.StatusBadRequest {
			t.Errorf("add %v: %d", bad, code)
		}
	}

	// Changing the datacenters recomputes the status from the last stock.
	_ = ts.store.Update(func(s *store.State) error {
		s.VPSWatch(v.ID).SetStock([]store.Stock{{Code: "eu-west-rbx", Name: "Roubaix", Available: true}}, time.Now())
		return nil
	})
	if code := ts.do("PATCH", "/api/vps/"+v.ID, map[string]any{"datacenters": []string{}, "paused": true, "note": "cheap"}, &v); code != http.StatusOK ||
		v.Status != store.Available || !v.Paused || v.Note != "cheap" || len(v.Datacenters) != 0 {
		t.Fatalf("edit: %d %+v", code, v)
	}
	if code := ts.do("POST", "/api/vps/"+v.ID+"/check", nil, nil); code != http.StatusAccepted {
		t.Fatalf("check: %d", code)
	}
	var st struct {
		VPS      []store.VPS
		Checking []string
	}
	ts.do("GET", "/api/state", nil, &st)
	if len(st.VPS) != 1 || len(st.Checking) != 1 || st.Checking[0] != "vps:"+v.ID {
		t.Fatalf("state = %+v", st)
	}
	if code := ts.do("DELETE", "/api/vps/"+v.ID, nil, nil); code != http.StatusOK {
		t.Fatalf("remove: %d", code)
	}
	if code := ts.do("PATCH", "/api/vps/"+v.ID, map[string]any{"paused": false}, nil); code != http.StatusNotFound {
		t.Fatalf("edit removed: %d", code)
	}

	// OVH plan codes are case-sensitive.
	var lz store.VPS
	if code := ts.do("POST", "/api/vps", map[string]any{"subsidiary": "FR", "plan": "vps-2027-model1.LZ"}, &lz); code != http.StatusOK || lz.Plan != "vps-2027-model1.LZ" {
		t.Fatalf("add a Local Zone plan: %d %+v", code, lz)
	}
}
