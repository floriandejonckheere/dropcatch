// Package hetzner reads the availability of Hetzner Cloud server types
// from the Hetzner Cloud API.
//
// The API needs a token, from any project; a read-only one is enough.
// GET /server_types lists each server type with its CPU type (shared or
// dedicated), architecture (x86 or arm), prices, and the locations that
// offer it, each with "available": whether it can be ordered there now.
package hetzner

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrNoToken means that no API token was configured.
var ErrNoToken = errors.New("Hetzner Cloud needs an API token: set DROPCATCH_HETZNER_TOKEN to a read-only token from any Hetzner Cloud project")

// OrderURL is where servers are created.
const OrderURL = "https://console.hetzner.com/"

// Location is the availability of a server type in one location.
type Location struct {
	Code      string `json:"code"` // fsn1, ash, …
	Name      string `json:"name"` // Falkenstein, Germany
	Region    string `json:"region"`
	Available bool   `json:"available"`
}

// Plan is a server type that can be ordered somewhere.
type Plan struct {
	Code         string  `json:"code"` // cx23
	Name         string  `json:"name"` // CX23
	CPUType      string  `json:"cpuType"`
	Architecture string  `json:"architecture"` // x86, arm
	Category     string  `json:"category,omitempty"`
	Cores        int     `json:"cores"`
	Memory       float64 `json:"memory"`  // GB
	Storage      int     `json:"storage"` // GB
	Disk         string  `json:"disk,omitempty"`
	Price        float64 `json:"price,omitempty"` // lowest monthly price, without tax
}

type Catalog struct {
	Currency string `json:"currency"`
	Plans    []Plan `json:"plans"`
}

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client

	userAgent string
	now       func() time.Time

	mu        sync.Mutex
	locations map[string]rawLocation
	locsAt    time.Time
	catalog   *Catalog
	catalogAt time.Time
}

func New(userAgent, token string) *Client {
	return &Client{
		BaseURL:   "https://api.hetzner.cloud/v1",
		Token:     token,
		HTTP:      &http.Client{Timeout: 30 * time.Second},
		userAgent: userAgent,
		now:       time.Now,
	}
}

// Configured reports whether a token is set.
func (c *Client) Configured() bool { return c.Token != "" }

type rawServerType struct {
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	Cores        int     `json:"cores"`
	Memory       float64 `json:"memory"`
	Disk         int     `json:"disk"`
	StorageType  any     `json:"storage_type"` // a string, or a list in newer responses
	CPUType      string  `json:"cpu_type"`
	Architecture string  `json:"architecture"`
	Category     string  `json:"category"`
	Prices       []struct {
		Location     string `json:"location"`
		PriceMonthly struct {
			Net string `json:"net"`
		} `json:"price_monthly"`
	} `json:"prices"`
	Locations []struct {
		Name        string       `json:"name"`
		Available   bool         `json:"available"`
		Deprecation *deprecation `json:"deprecation"`
	} `json:"locations"`
}

type deprecation struct {
	UnavailableAfter time.Time `json:"unavailable_after"`
}

type rawLocation struct {
	Name        string `json:"name"`
	City        string `json:"city"`
	Country     string `json:"country"`
	NetworkZone string `json:"network_zone"`
}

func (c *Client) get(ctx context.Context, path string, query url.Values, v any) error {
	if c.Token == "" {
		return ErrNoToken
	}
	u := strings.TrimSuffix(c.BaseURL, "/") + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusUnauthorized:
		return errors.New("Hetzner refused the API token (401); check DROPCATCH_HETZNER_TOKEN")
	case res.StatusCode == http.StatusTooManyRequests:
		return errors.New("rate limited by Hetzner (429)")
	case res.StatusCode != http.StatusOK:
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
			return fmt.Errorf("Hetzner: %s (%d)", e.Error.Message, res.StatusCode)
		}
		return fmt.Errorf("Hetzner answered %d", res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(v)
}

// serverTypes lists the server types, all of them or the named one.
func (c *Client) serverTypes(ctx context.Context, name string) ([]rawServerType, error) {
	var out []rawServerType
	for page := 1; page > 0; {
		q := url.Values{"per_page": {"50"}, "page": {strconv.Itoa(page)}}
		if name != "" {
			q.Set("name", name)
		}
		var res struct {
			ServerTypes []rawServerType `json:"server_types"`
			Meta        struct {
				Pagination struct {
					NextPage int `json:"next_page"`
				} `json:"pagination"`
			} `json:"meta"`
		}
		if err := c.get(ctx, "/server_types", q, &res); err != nil {
			return nil, err
		}
		out = append(out, res.ServerTypes...)
		page = res.Meta.Pagination.NextPage
	}
	return out, nil
}

// locationsByName returns Hetzner's locations, cached for a day: new
// locations are rare, and the server types only name them.
func (c *Client) locationsByName(ctx context.Context) (map[string]rawLocation, error) {
	c.mu.Lock()
	if c.locations != nil && c.now().Sub(c.locsAt) < 24*time.Hour {
		defer c.mu.Unlock()
		return c.locations, nil
	}
	c.mu.Unlock()
	var res struct {
		Locations []rawLocation `json:"locations"`
	}
	if err := c.get(ctx, "/locations", url.Values{"per_page": {"50"}}, &res); err != nil {
		return nil, err
	}
	m := map[string]rawLocation{}
	for _, l := range res.Locations {
		m[l.Name] = l
	}
	c.mu.Lock()
	c.locations, c.locsAt = m, c.now()
	c.mu.Unlock()
	return m, nil
}

// offered reports whether a server type can still be ordered in a
// location: Hetzner keeps listing it there until its deprecation date.
func (c *Client) offered(d *deprecation) bool {
	return d == nil || d.UnavailableAfter.IsZero() || c.now().Before(d.UnavailableAfter)
}

// Stock returns the locations that offer the server type, and whether it
// is available in each, sorted by region and name.
func (c *Client) Stock(ctx context.Context, plan string) ([]Location, error) {
	types, err := c.serverTypes(ctx, plan)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(types, func(t rawServerType) bool { return t.Name == plan })
	if i < 0 {
		return nil, fmt.Errorf("Hetzner has no server type %s", plan)
	}
	locs, err := c.locationsByName(ctx)
	if err != nil {
		return nil, err
	}
	out := []Location{}
	for _, l := range types[i].Locations {
		if !c.offered(l.Deprecation) {
			continue
		}
		info := locs[l.Name]
		out = append(out, Location{Code: l.Name, Name: locationName(l.Name, info), Region: regionOf(info.NetworkZone), Available: l.Available})
	}
	slices.SortFunc(out, func(a, b Location) int {
		return cmp.Or(cmp.Compare(regionOrder(a.Region), regionOrder(b.Region)), cmp.Compare(a.Name, b.Name))
	})
	return out, nil
}

// Plans returns the server types that can be ordered in at least one
// location, cached for an hour.
func (c *Client) Plans(ctx context.Context) (Catalog, error) {
	c.mu.Lock()
	if c.catalog != nil && c.now().Sub(c.catalogAt) < time.Hour {
		defer c.mu.Unlock()
		return *c.catalog, nil
	}
	c.mu.Unlock()
	types, err := c.serverTypes(ctx, "")
	if err != nil {
		return Catalog{}, err
	}
	cat := Catalog{Currency: "EUR", Plans: []Plan{}}
	for _, t := range types {
		offered := map[string]bool{}
		for _, l := range t.Locations {
			if c.offered(l.Deprecation) {
				offered[l.Name] = true
			}
		}
		if len(offered) == 0 {
			continue
		}
		p := Plan{
			Code: t.Name, Name: cmp.Or(t.Description, strings.ToUpper(t.Name)),
			CPUType: t.CPUType, Architecture: t.Architecture, Category: t.Category,
			Cores: t.Cores, Memory: t.Memory, Storage: t.Disk, Disk: storage(t.StorageType),
		}
		for _, pr := range t.Prices {
			if price, err := strconv.ParseFloat(pr.PriceMonthly.Net, 64); err == nil && offered[pr.Location] && (p.Price == 0 || price < p.Price) {
				p.Price = price
			}
		}
		cat.Plans = append(cat.Plans, p)
	}
	// Grouped as the form shows them: shared before dedicated, x86
	// before Arm, then by size.
	slices.SortFunc(cat.Plans, func(a, b Plan) int {
		return cmp.Or(cmp.Compare(b.CPUType, a.CPUType), cmp.Compare(b.Architecture, a.Architecture), cmp.Compare(a.Price, b.Price), cmp.Compare(a.Code, b.Code))
	})
	c.mu.Lock()
	c.catalog, c.catalogAt = &cat, c.now()
	c.mu.Unlock()
	return cat, nil
}

func storage(v any) string {
	switch s := v.(type) {
	case string:
		if s == "local" {
			return "local SSD"
		}
		return s
	case []any:
		for _, x := range s {
			if x == "local" {
				return "local SSD"
			}
		}
	}
	return ""
}

var countries = map[string]string{"DE": "Germany", "FI": "Finland", "US": "United States", "SG": "Singapore"}

// locationName returns "Falkenstein, Germany" or "Singapore", or the code when the
// location is unknown.
func locationName(code string, l rawLocation) string {
	if l.City == "" {
		return strings.ToUpper(code)
	}
	// US cities come with their state ("Ashburn, VA"); the country is
	// shown instead, as for the other locations.
	city, _, _ := strings.Cut(l.City, ",")
	country := cmp.Or(countries[l.Country], l.Country)
	if country == city || country == "" {
		return city
	}
	return city + ", " + country
}

// regionOf maps a network zone (eu-central, us-east, us-west,
// ap-southeast) to the regions the form groups locations by.
func regionOf(zone string) string {
	switch prefix, _, _ := strings.Cut(zone, "-"); prefix {
	case "eu":
		return "europe"
	case "us", "ca":
		return "north-america"
	case "ap":
		return "asia-pacific"
	}
	return "other"
}

func regionOrder(r string) int {
	return slices.Index([]string{"europe", "north-america", "asia-pacific", "other"}, r)
}
