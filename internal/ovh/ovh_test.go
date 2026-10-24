package ovh

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeAPI(t *testing.T) (*Client, *int) {
	catalogs := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("ovhSubsidiary") != "FR" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"message":"Invalid subsidiary"}`)
			return
		}
		switch r.URL.Path {
		case "/v1/vps/order/rule/datacenter":
			if q.Get("planCode") != "vps-2025-model1" {
				fmt.Fprint(w, `{"datacenters":[]}`)
				return
			}
			fmt.Fprint(w, `{"datacenters":[
				{"datacenter":"GRA","code":"eu-west-gra","status":"available","linuxStatus":"available","windowsStatus":"out-of-stock","daysBeforeDelivery":0},
				{"datacenter":"SBG","code":"eu-west-sbg","status":"available","linuxStatus":"out-of-stock"},
				{"datacenter":"BHS","code":"ca-east-bhs","status":"out-of-stock","linuxStatus":"out-of-stock"},
				{"datacenter":"SGP","code":"ap-southeast-sgp","status":"available"}]}`)
		case "/v1/order/catalog/public/vps":
			catalogs++
			fmt.Fprint(w, `{"locale":{"currencyCode":"EUR"},
				"plans":[
					{"planCode":"vps-2025-model2","invoiceName":"VPS-2","product":"vps-2025-model2","blobs":{"tags":["order-funnel:show"]},
					 "pricings":[{"capacities":["installation"],"interval":1,"intervalUnit":"month","price":0},
					             {"capacities":["renew"],"interval":1,"intervalUnit":"month","price":770000000}]},
					{"planCode":"vps-2025-model10","invoiceName":"VPS-10","blobs":{"tags":["order-funnel:show"]}},
					{"planCode":"vps-2025-model1","invoiceName":"VPS-1","product":"vps-2025-model1",
					 "pricings":[{"capacities":["renew"],"interval":1,"intervalUnit":"month","price":420000000}],
					 "blobs":{"tags":["order-funnel:show"],"technical":{"cpu":{"cores":4},"memory":{"size":8},"storage":{"disks":[{"capacity":75,"technology":"SSD"}]}}}},
					{"planCode":"vps-2025-model1-eu","invoiceName":"VPS-1","blobs":{"tags":["order-funnel:show"]}},
					{"planCode":"vps-2024-model2","invoiceName":"VPS-2 2024","blobs":{"tags":["os:distribution-only"]}},
					{"planCode":"vps-essential-2-4-80-vps-2025-model2","invoiceName":"VPS-2"},
					{"planCode":"option-backup","invoiceName":"Backup","blobs":{"tags":["order-funnel:show"]}}],
				"products":[{"name":"vps-2025-model2","blobs":{"technical":{"cpu":{"cores":6},"memory":{"size":12},"storage":{"disks":[{"capacity":100,"technology":"NVMe"}]}}}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := New("test")
	c.BaseURL = srv.URL + "/v1"
	return c, &catalogs
}

func TestStock(t *testing.T) {
	c, _ := fakeAPI(t)
	ctx := context.Background()
	dcs, err := c.Stock(ctx, "FR", "vps-2025-model1")
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, d := range dcs {
		got = append(got, fmt.Sprintf("%s:%s:%v", d.Code, d.Region, d.Available))
	}
	// Sorted by name; Linux images out of stock count as out of stock.
	want := "ca-east-bhs:north-america:false eu-west-gra:europe:true ap-southeast-sgp:asia-pacific:true eu-west-sbg:europe:false"
	if strings.Join(got, " ") != want {
		t.Fatalf("stock = %v", got)
	}

	if _, err := c.Stock(ctx, "FR", "vps-unknown"); err == nil {
		t.Fatal("plan without datacenters accepted")
	}
	if _, err := c.Stock(ctx, "DE", "vps-2025-model1"); err == nil || !strings.Contains(err.Error(), "Invalid subsidiary") {
		t.Fatalf("OVH error message not reported: %v", err)
	}
}

func TestPlans(t *testing.T) {
	c, fetched := fakeAPI(t)
	cat, err := c.Plans(context.Background(), "FR")
	if err != nil {
		t.Fatal(err)
	}
	// Only orderable plans, sorted by name; plans sharing a name show
	// their code.
	want := []Plan{
		{Code: "vps-2025-model1", Name: "VPS-1 (vps-2025-model1)", Cores: 4, Memory: 8, Storage: 75, Disk: "SSD", Price: 4.2},
		{Code: "vps-2025-model1-eu", Name: "VPS-1 (vps-2025-model1-eu)"},
		{Code: "vps-2025-model2", Name: "VPS-2", Cores: 6, Memory: 12, Storage: 100, Disk: "NVMe", Price: 7.7},
		{Code: "vps-2025-model10", Name: "VPS-10"},
	}
	if cat.Currency != "EUR" || len(cat.Plans) != len(want) {
		t.Fatalf("catalog = %+v", cat)
	}
	for i := range want {
		if cat.Plans[i] != want[i] {
			t.Errorf("plan %d = %+v, want %+v", i, cat.Plans[i], want[i])
		}
	}
	_, _ = c.Plans(context.Background(), "FR")
	if *fetched != 1 {
		t.Errorf("catalog fetched %d times, want 1 (cached)", *fetched)
	}
}

func TestHelpers(t *testing.T) {
	if apiBase("US") != "https://api.us.ovhcloud.com/v1" || apiBase("QC") != "https://ca.api.ovh.com/v1" || apiBase("FR") != "https://eu.api.ovh.com/v1" {
		t.Error("wrong API hosts")
	}
	if !ValidSubsidiary("FR") || ValidSubsidiary("XX") {
		t.Error("ValidSubsidiary")
	}
	if DatacenterName("eu-west-gra") != "Gravelines, France" || DatacenterName("xx-new-dc") != "XX-NEW-DC" {
		t.Error("DatacenterName")
	}
}
