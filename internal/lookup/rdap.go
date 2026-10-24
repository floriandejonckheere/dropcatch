package lookup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// rdapServer returns the RDAP base URL for name, or "" when its TLD has
// none. A stale bootstrap file is used when IANA cannot be reached.
func (c *Client) rdapServer(ctx context.Context, name string) (string, error) {
	c.mu.Lock()
	fresh := c.rdap != nil && time.Since(c.rdapFetched) < bootstrapTTL
	c.mu.Unlock()
	if !fresh {
		services, err := c.fetchBootstrap(ctx)
		c.mu.Lock()
		switch {
		case err == nil:
			c.rdap, c.rdapFetched = services, time.Now()
		case c.rdap == nil:
			c.mu.Unlock()
			return "", fmt.Errorf("fetching the RDAP server list: %w", err)
		default:
			slog.Warn("using a stale RDAP server list", "err", err)
		}
		c.mu.Unlock()
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	// Entries are TLDs, but RFC 9224 allows longer suffixes: the longest wins.
	labels := strings.Split(name, ".")
	for i := 1; i < len(labels); i++ {
		if base, ok := c.rdap[strings.Join(labels[i:], ".")]; ok {
			return base, nil
		}
	}
	return "", nil
}

// fetchBootstrap downloads the IANA RDAP bootstrap file (RFC 9224).
func (c *Client) fetchBootstrap(ctx context.Context) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BootstrapURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", c.BootstrapURL, res.Status)
	}
	var file struct {
		Services [][][]string `json:"services"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxResponse)).Decode(&file); err != nil {
		return nil, fmt.Errorf("%s: %w", c.BootstrapURL, err)
	}
	services := map[string]string{}
	for _, s := range file.Services {
		if len(s) != 2 {
			continue
		}
		// Prefer HTTPS when a registry lists several URLs.
		urls := slices.Clone(s[1])
		slices.SortStableFunc(urls, func(a, b string) int {
			return boolInt(!strings.HasPrefix(a, "https:")) - boolInt(!strings.HasPrefix(b, "https:"))
		})
		if len(urls) == 0 {
			continue
		}
		for _, suffix := range s[0] {
			services[strings.ToLower(suffix)] = urls[0]
		}
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("%s lists no services", c.BootstrapURL)
	}
	return services, nil
}

type rdapDomain struct {
	Status []string `json:"status"`
	Events []struct {
		Action string `json:"eventAction"`
		Date   string `json:"eventDate"`
	} `json:"events"`
	Entities []struct {
		Roles []string          `json:"roles"`
		VCard []json.RawMessage `json:"vcardArray"`
	} `json:"entities"`
}

func (c *Client) queryRDAP(ctx context.Context, base, name string) (Result, error) {
	u, err := url.JoinPath(base, "domain", name)
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Accept", "application/rdap+json, application/json")
	req.Header.Set("User-Agent", c.UserAgent)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer res.Body.Close()
	r := Result{Source: "rdap", Server: req.URL.Host}

	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		// RFC 9082: the registry has no such object.
		r.Status = Available
		return r, nil
	case http.StatusTooManyRequests:
		return Result{}, fmt.Errorf("%s: rate limited, try a longer check interval", r.Server)
	default:
		return Result{}, fmt.Errorf("%s: %s", r.Server, res.Status)
	}

	var d rdapDomain
	if err := json.NewDecoder(io.LimitReader(res.Body, maxResponse)).Decode(&d); err != nil {
		return Result{}, fmt.Errorf("%s: invalid response: %w", r.Server, err)
	}
	r.Status = Registered
	for _, s := range d.Status {
		s = strings.ToLower(strings.TrimSpace(s))
		r.Statuses = append(r.Statuses, s)
		if dropping(s) {
			r.Status = Dropping
		}
	}
	for _, e := range d.Events {
		t, err := time.Parse(time.RFC3339, e.Date)
		if err != nil {
			continue
		}
		switch strings.ToLower(e.Action) {
		case "registration":
			r.Created = t
		case "expiration":
			r.Expires = t
		}
	}
	for _, e := range d.Entities {
		if slices.Contains(e.Roles, "registrar") {
			r.Registrar = vcardName(e.VCard)
			break
		}
	}
	return r, nil
}

// dropping reports whether a registry status (RDAP or EPP spelling) means
// the domain is about to be deleted.
func dropping(status string) bool {
	s := strings.ToLower(strings.NewReplacer(" ", "", "_", "", "-", "").Replace(status))
	return s == "pendingdelete" || s == "redemptionperiod" || s == "quarantine"
}

// vcardName extracts the "fn" property from a jCard (RFC 7095):
// ["vcard", [["fn", {}, "text", "Example Registrar"], ...]].
func vcardName(card []json.RawMessage) string {
	if len(card) != 2 {
		return ""
	}
	var props [][]any
	if json.Unmarshal(card[1], &props) != nil {
		return ""
	}
	for _, p := range props {
		if len(p) == 4 && p[0] == "fn" {
			if s, ok := p[3].(string); ok {
				return s
			}
		}
	}
	return ""
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
