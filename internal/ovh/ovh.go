// Package ovh reads VPS stock and plans from OVHcloud's public API, which
// needs no account or credentials.
//
// The stock of a plan comes from /vps/order/rule/datacenter, which lists
// each datacenter that offers the plan with "available" or "out-of-stock".
// Plans come from the public order catalog. Both depend on the OVH
// subsidiary (the country shop), which also picks the API host.
package ovh

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// Subsidiary is an OVH country shop.
type Subsidiary struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// Subsidiaries that sell VPS. US is a separate company (OVHcloud US) with
// its own API and datacenters; the others share the European and Canadian
// datacenters.
var Subsidiaries = []Subsidiary{
	{"FR", "France"}, {"DE", "Germany"}, {"GB", "United Kingdom"}, {"IE", "Ireland"},
	{"ES", "Spain"}, {"IT", "Italy"}, {"NL", "Netherlands"}, {"PL", "Poland"},
	{"PT", "Portugal"}, {"CZ", "Czech Republic"}, {"FI", "Finland"}, {"LT", "Lithuania"},
	{"MA", "Morocco"}, {"SN", "Senegal"}, {"TN", "Tunisia"}, {"WE", "Western Europe (English)"},
	{"CA", "Canada (English)"}, {"QC", "Canada (French)"}, {"WS", "World (English)"},
	{"ASIA", "Asia"}, {"AU", "Australia"}, {"SG", "Singapore"}, {"IN", "India"},
	{"US", "United States"},
}

// ValidSubsidiary reports whether code is a known subsidiary.
func ValidSubsidiary(code string) bool {
	return slices.ContainsFunc(Subsidiaries, func(s Subsidiary) bool { return s.Code == code })
}

// apiBase returns the API root for a subsidiary: OVH runs one API per
// region, and each only serves its own subsidiaries.
func apiBase(subsidiary string) string {
	switch subsidiary {
	case "US":
		return "https://api.us.ovhcloud.com/v1"
	case "CA", "QC", "WS", "ASIA", "AU", "SG", "IN":
		return "https://ca.api.ovh.com/v1"
	}
	return "https://eu.api.ovh.com/v1"
}

// OrderURL returns the page where plan can be ordered.
func OrderURL(subsidiary, plan string) string {
	if subsidiary == "US" {
		return "https://us.ovhcloud.com/vps/"
	}
	return "https://www.ovhcloud.com/en/vps/configurator/?planCode=" + url.QueryEscape(plan)
}

// Region groups datacenters for selection.
type Region string

const (
	Europe       Region = "europe"
	NorthAmerica Region = "north-america"
	AsiaPacific  Region = "asia-pacific"
	OtherRegion  Region = "other"
)

// RegionOf returns the region of a datacenter code such as "eu-west-gra".
func RegionOf(code string) Region {
	switch prefix, _, _ := strings.Cut(code, "-"); prefix {
	case "eu":
		return Europe
	case "ca", "us":
		return NorthAmerica
	case "ap":
		return AsiaPacific
	}
	return OtherRegion
}

var datacenterNames = map[string]string{
	"eu-west-gra":      "Gravelines, France",
	"eu-west-rbx":      "Roubaix, France",
	"eu-west-sbg":      "Strasbourg, France",
	"eu-west-par":      "Paris, France",
	"eu-west-lim":      "Limburg, Germany",
	"eu-west-eri":      "London, United Kingdom",
	"eu-central-waw":   "Warsaw, Poland",
	"eu-south-mil":     "Milan, Italy",
	"ca-east-bhs":      "Beauharnois, Canada",
	"ca-east-tor":      "Toronto, Canada",
	"us-east-vin":      "Vint Hill, United States",
	"us-west-hil":      "Hillsboro, United States",
	"ap-southeast-sgp": "Singapore",
	"ap-southeast-syd": "Sydney, Australia",
	"ap-south-mum":     "Mumbai, India",
}

// DatacenterName returns a readable name for a datacenter code.
func DatacenterName(code string) string {
	if n, ok := datacenterNames[code]; ok {
		return n
	}
	return strings.ToUpper(code)
}

// Datacenter is the stock of a plan in one datacenter.
type Datacenter struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Region    Region `json:"region"`
	Available bool   `json:"available"`
}

// Plan is a VPS model from the catalog.
type Plan struct {
	Code    string  `json:"code"`
	Name    string  `json:"name"`
	Cores   int     `json:"cores,omitempty"`
	Memory  int     `json:"memory,omitempty"`  // GB
	Storage int     `json:"storage,omitempty"` // GB
	Disk    string  `json:"disk,omitempty"`    // SSD, NVMe
	Price   float64 `json:"price,omitempty"`   // per month, without tax
}

// Catalog is the list of VPS plans of a subsidiary.
type Catalog struct {
	Currency string `json:"currency"`
	Plans    []Plan `json:"plans"`
}

const (
	catalogTTL  = 6 * time.Hour
	maxResponse = 16 << 20
)

type Client struct {
	HTTP *http.Client
	// BaseURL replaces the API host of every subsidiary (for tests and the
	// fake registry); empty uses OVH's.
	BaseURL   string
	UserAgent string

	mu       sync.Mutex
	catalogs map[string]cachedCatalog
}

type cachedCatalog struct {
	catalog Catalog
	fetched time.Time
}

func New(userAgent string) *Client {
	return &Client{HTTP: &http.Client{}, UserAgent: userAgent}
}

func (c *Client) get(ctx context.Context, subsidiary, path string, query url.Values, v any) error {
	base := c.BaseURL
	if base == "" {
		base = apiBase(subsidiary)
	}
	u := strings.TrimSuffix(base, "/") + path + "?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("%s: rate limited, try a longer check interval", req.URL.Host)
	case res.StatusCode != http.StatusOK:
		// OVH explains errors in {"message": "..."}.
		var e struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&e)
		if e.Message != "" {
			return fmt.Errorf("%s: %s", req.URL.Host, e.Message)
		}
		return fmt.Errorf("%s: %s", req.URL.Host, res.Status)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxResponse)).Decode(v); err != nil {
		return fmt.Errorf("%s: invalid response: %w", req.URL.Host, err)
	}
	return nil
}

// Stock returns the datacenters that offer plan, with their stock.
func (c *Client) Stock(ctx context.Context, subsidiary, plan string) ([]Datacenter, error) {
	var res struct {
		Datacenters []struct {
			Datacenter  string `json:"datacenter"`
			Code        string `json:"code"`
			Status      string `json:"status"`
			LinuxStatus string `json:"linuxStatus"`
		} `json:"datacenters"`
	}
	q := url.Values{"ovhSubsidiary": {subsidiary}, "planCode": {plan}}
	if err := c.get(ctx, subsidiary, "/vps/order/rule/datacenter", q, &res); err != nil {
		return nil, err
	}
	if len(res.Datacenters) == 0 {
		return nil, fmt.Errorf("OVH offers %s in no datacenter; check the plan and subsidiary", plan)
	}
	out := make([]Datacenter, 0, len(res.Datacenters))
	for _, d := range res.Datacenters {
		code := strings.ToLower(d.Code)
		if code == "" {
			code = strings.ToLower(d.Datacenter)
		}
		out = append(out, Datacenter{
			Code:   code,
			Name:   DatacenterName(code),
			Region: RegionOf(code),
			// "status" covers the plan; "linuxStatus" the Linux images,
			// which can run out separately. Without Linux, a VPS is of
			// little use, so both must be available.
			Available: d.Status == "available" && (d.LinuxStatus == "" || d.LinuxStatus == "available"),
		})
	}
	slices.SortFunc(out, func(a, b Datacenter) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// Plans returns the VPS plans of a subsidiary, cheapest first.
func (c *Client) Plans(ctx context.Context, subsidiary string) (Catalog, error) {
	c.mu.Lock()
	cached, ok := c.catalogs[subsidiary]
	c.mu.Unlock()
	if ok && time.Since(cached.fetched) < catalogTTL {
		return cached.catalog, nil
	}

	var raw rawCatalog
	if err := c.get(ctx, subsidiary, "/order/catalog/public/vps", url.Values{"ovhSubsidiary": {subsidiary}}, &raw); err != nil {
		return Catalog{}, err
	}
	cat := raw.parse()
	if len(cat.Plans) == 0 {
		return Catalog{}, fmt.Errorf("the OVH catalog for %s lists no VPS plans", subsidiary)
	}
	c.mu.Lock()
	if c.catalogs == nil {
		c.catalogs = map[string]cachedCatalog{}
	}
	c.catalogs[subsidiary] = cachedCatalog{cat, time.Now()}
	c.mu.Unlock()
	return cat, nil
}

type technical struct {
	CPU struct {
		Cores int `json:"cores"`
	} `json:"cpu"`
	Memory struct {
		Size int `json:"size"`
	} `json:"memory"`
	Storage struct {
		Disks []struct {
			Capacity   int    `json:"capacity"`
			Technology string `json:"technology"`
		} `json:"disks"`
	} `json:"storage"`
}

type rawCatalog struct {
	Locale struct {
		CurrencyCode string `json:"currencyCode"`
	} `json:"locale"`
	Plans []struct {
		PlanCode    string `json:"planCode"`
		InvoiceName string `json:"invoiceName"`
		Product     string `json:"product"`
		Pricings    []struct {
			Capacities   []string `json:"capacities"`
			Interval     int      `json:"interval"`
			IntervalUnit string   `json:"intervalUnit"`
			Price        int64    `json:"price"`
		} `json:"pricings"`
		Blobs struct {
			Technical *technical `json:"technical"`
		} `json:"blobs"`
	} `json:"plans"`
	Products []struct {
		Name  string `json:"name"`
		Blobs struct {
			Technical *technical `json:"technical"`
		} `json:"blobs"`
	} `json:"products"`
}

// parse keeps the VPS plans and their specifications. Catalog prices are
// in hundred-millionths of the currency unit.
func (r rawCatalog) parse() Catalog {
	products := map[string]*technical{}
	for _, p := range r.Products {
		products[p.Name] = p.Blobs.Technical
	}
	cat := Catalog{Currency: r.Locale.CurrencyCode, Plans: []Plan{}}
	for _, p := range r.Plans {
		if !strings.HasPrefix(p.PlanCode, "vps") {
			continue
		}
		plan := Plan{Code: p.PlanCode, Name: p.InvoiceName}
		if plan.Name == "" {
			plan.Name = p.PlanCode
		}
		for _, pr := range p.Pricings {
			if slices.Contains(pr.Capacities, "renew") && pr.IntervalUnit == "month" && pr.Interval == 1 {
				plan.Price = float64(pr.Price) / 1e8
				break
			}
		}
		tech := p.Blobs.Technical
		if tech == nil {
			tech = products[p.Product]
		}
		if tech != nil {
			plan.Cores, plan.Memory = tech.CPU.Cores, tech.Memory.Size
			if len(tech.Storage.Disks) > 0 {
				plan.Storage, plan.Disk = tech.Storage.Disks[0].Capacity, tech.Storage.Disks[0].Technology
			}
		}
		cat.Plans = append(cat.Plans, plan)
	}
	// Plans without a monthly price go last.
	price := func(p Plan) float64 {
		if p.Price == 0 {
			return math.Inf(1)
		}
		return p.Price
	}
	slices.SortStableFunc(cat.Plans, func(a, b Plan) int {
		return cmp.Or(cmp.Compare(price(a), price(b)), strings.Compare(a.Code, b.Code))
	})
	return cat
}
