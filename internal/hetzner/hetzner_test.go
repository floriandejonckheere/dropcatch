package hetzner

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A trimmed response of GET /server_types, in the format of October 2026.
const serverTypes = `{"server_types":[
 {"name":"cx23","description":"CX23","cores":2,"memory":4.0,"disk":40,"storage_type":"local","cpu_type":"shared","architecture":"x86","category":"cost_optimized",
  "prices":[{"location":"fsn1","price_monthly":{"net":"3.4900","gross":"4.1531"}},{"location":"hel1","price_monthly":{"net":"3.2900","gross":"3.9151"}}],
  "locations":[{"id":1,"name":"fsn1","available":false,"recommended":false,"deprecation":null},{"id":3,"name":"hel1","available":true,"recommended":true,"deprecation":null}]},
 {"name":"cax11","description":"CAX11","cores":2,"memory":4.0,"disk":40,"storage_type":["local"],"cpu_type":"shared","architecture":"arm","category":"cost_optimized",
  "prices":[{"location":"fsn1","price_monthly":{"net":"3.7900","gross":"4.5101"}}],
  "locations":[{"id":1,"name":"fsn1","available":true,"recommended":false,"deprecation":null}]},
 {"name":"ccx13","description":"CCX13","cores":2,"memory":8.0,"disk":80,"storage_type":"local","cpu_type":"dedicated","architecture":"x86","category":"general_purpose",
  "prices":[{"location":"ash","price_monthly":{"net":"12.4900","gross":"12.4900"}},{"location":"sin","price_monthly":{"net":"14.4900","gross":"14.4900"}}],
  "locations":[{"id":4,"name":"ash","available":true,"recommended":true,"deprecation":null},{"id":6,"name":"sin","available":false,"recommended":false,"deprecation":null}]},
 {"name":"cx22","description":"CX22","cores":2,"memory":4.0,"disk":40,"storage_type":"local","cpu_type":"shared","architecture":"x86","category":"cost_optimized",
  "prices":[{"location":"fsn1","price_monthly":{"net":"3.2900","gross":"3.9151"}}],
  "locations":[{"id":1,"name":"fsn1","available":true,"recommended":false,"deprecation":{"announced":"2025-10-16T00:00:00Z","unavailable_after":"2026-01-01T00:00:00Z"}}]}
],"meta":{"pagination":{"page":1,"per_page":50,"previous_page":null,"next_page":null,"last_page":1,"total_entries":4}}}`

const locations = `{"locations":[
 {"id":1,"name":"fsn1","description":"Falkenstein DC Park 1","country":"DE","city":"Falkenstein","network_zone":"eu-central"},
 {"id":3,"name":"hel1","description":"Helsinki DC Park 1","country":"FI","city":"Helsinki","network_zone":"eu-central"},
 {"id":4,"name":"ash","description":"Ashburn, VA","country":"US","city":"Ashburn, VA","network_zone":"us-east"},
 {"id":6,"name":"sin","description":"Singapore","country":"SG","city":"Singapore","network_zone":"ap-southeast"}]}`

func newTestClient(t *testing.T) (*Client, *[]string) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		calls = append(calls, r.URL.Path+"?"+r.URL.RawQuery)
		switch r.URL.Path {
		case "/v1/server_types":
			if name := r.URL.Query().Get("name"); name == "nope" {
				_, _ = w.Write([]byte(`{"server_types":[],"meta":{"pagination":{"next_page":null}}}`))
				return
			}
			_, _ = w.Write([]byte(serverTypes))
		case "/v1/locations":
			_, _ = w.Write([]byte(locations))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c := New("test", "secret")
	c.BaseURL = srv.URL + "/v1"
	c.now = func() time.Time { return time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC) }
	return c, &calls
}

func TestStock(t *testing.T) {
	c, calls := newTestClient(t)
	got, err := c.Stock(context.Background(), "ccx13")
	if err != nil {
		t.Fatal(err)
	}
	want := []Location{
		{Code: "ash", Name: "Ashburn, United States", Region: "north-america", Available: true},
		{Code: "sin", Name: "Singapore", Region: "asia-pacific", Available: false},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("stock = %+v", got)
	}
	if (*calls)[0] != "/v1/server_types?name=ccx13&page=1&per_page=50" {
		t.Errorf("calls = %v", *calls)
	}

	// Locations where the type is deprecated are not offered anymore.
	if got, _ := c.Stock(context.Background(), "cx22"); len(got) != 0 {
		t.Errorf("deprecated type offered in %+v", got)
	}
	if _, err := c.Stock(context.Background(), "nope"); err == nil || !strings.Contains(err.Error(), "no server type") {
		t.Errorf("unknown type: %v", err)
	}
	// Locations are cached.
	n := 0
	for _, call := range *calls {
		if strings.HasPrefix(call, "/v1/locations") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("locations fetched %d times", n)
	}
}

func TestPlans(t *testing.T) {
	c, _ := newTestClient(t)
	cat, err := c.Plans(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	for _, p := range cat.Plans {
		codes = append(codes, p.Code)
	}
	// Shared x86, shared Arm, dedicated; the retired cx22 is left out.
	if strings.Join(codes, ",") != "cx23,cax11,ccx13" {
		t.Fatalf("plans = %v", codes)
	}
	p := cat.Plans[0]
	if p.Name != "CX23" || p.Price != 3.29 || p.Memory != 4 || p.Storage != 40 || p.Disk != "local SSD" || p.CPUType != "shared" || p.Architecture != "x86" {
		t.Errorf("plan = %+v", p)
	}
	if cat.Plans[1].Disk != "local SSD" {
		t.Errorf("storage_type list not read: %+v", cat.Plans[1])
	}
}

func TestErrors(t *testing.T) {
	if _, err := New("test", "").Stock(context.Background(), "cx23"); !errors.Is(err, ErrNoToken) {
		t.Errorf("without token: %v", err)
	}
	c, _ := newTestClient(t)
	c.Token = "wrong"
	if _, err := c.Plans(context.Background()); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("wrong token: %v", err)
	}
}
