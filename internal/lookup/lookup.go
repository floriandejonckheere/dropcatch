// Package lookup asks domain registries whether a domain is registered.
//
// It uses RDAP, the protocol ICANN requires of gTLD registries, with the
// IANA bootstrap file to find each TLD's server. TLDs without RDAP (several
// ccTLDs) are queried over WHOIS, with the server found through
// whois.iana.org.
package lookup

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

type Status string

const (
	Registered Status = "registered"
	// Dropping means the registration expired and the registry is about to
	// delete it (redemption or pending delete period).
	Dropping  Status = "dropping"
	Available Status = "available"
)

// Result is what the registry reported about a domain.
type Result struct {
	Status    Status
	Statuses  []string // registry status codes, such as "client transfer prohibited"
	Registrar string
	Created   time.Time
	Expires   time.Time
	Source    string // "rdap" or "whois"
	Server    string // host that answered
}

const (
	DefaultBootstrap = "https://data.iana.org/rdap/dns.json"
	DefaultIANAWhois = "whois.iana.org:43"

	// How long the RDAP bootstrap file and WHOIS server lookups are reused.
	bootstrapTTL = 24 * time.Hour
	// Largest response read from a registry.
	maxResponse = 2 << 20
)

// ErrNoServer means neither an RDAP nor a WHOIS server is known for the TLD.
var ErrNoServer = errors.New("no RDAP or WHOIS server is known for this TLD")

type Client struct {
	HTTP         *http.Client
	BootstrapURL string
	IANAWhois    string // host:port
	Dial         func(ctx context.Context, network, addr string) (net.Conn, error)
	UserAgent    string

	mu          sync.Mutex
	rdap        map[string]string // TLD (or longer suffix) -> base URL
	rdapFetched time.Time
	whois       map[string]whoisServer // TLD -> server
}

type whoisServer struct {
	addr    string // empty when IANA lists none
	fetched time.Time
}

func New(userAgent string) *Client {
	d := &net.Dialer{}
	return &Client{
		HTTP:         &http.Client{},
		BootstrapURL: DefaultBootstrap,
		IANAWhois:    DefaultIANAWhois,
		Dial:         d.DialContext,
		UserAgent:    userAgent,
	}
}

// Lookup asks the registry of name (as returned by Normalize) about it.
func (c *Client) Lookup(ctx context.Context, name string) (Result, error) {
	base, err := c.rdapServer(ctx, name)
	if err != nil {
		return Result{}, err
	}
	if base != "" {
		return c.queryRDAP(ctx, base, name)
	}
	addr, err := c.whoisServer(ctx, tld(name))
	if err != nil {
		return Result{}, err
	}
	if addr == "" {
		return Result{}, ErrNoServer
	}
	return c.queryWhois(ctx, addr, name)
}

// Normalize turns user input (a domain name, possibly with a scheme, path,
// trailing dot or non-ASCII characters) into the lowercase ASCII form used
// by registries.
func Normalize(s string) (string, error) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "://"); i >= 0 {
		if u, err := url.Parse(s); err == nil && u.Hostname() != "" {
			s = u.Hostname()
		}
	}
	s = strings.ToLower(strings.TrimSuffix(s, "."))
	if s == "" {
		return "", errors.New("empty domain name")
	}
	if net.ParseIP(s) != nil {
		return "", fmt.Errorf("%q is an IP address, not a domain name", s)
	}
	ascii, err := idna.Lookup.ToASCII(s)
	if err != nil {
		return "", fmt.Errorf("%q is not a valid domain name", s)
	}
	labels := strings.Split(ascii, ".")
	if len(labels) < 2 {
		return "", fmt.Errorf("%q has no top-level domain", s)
	}
	for _, l := range labels {
		if l == "" || len(l) > 63 || strings.HasPrefix(l, "-") || strings.HasSuffix(l, "-") {
			return "", fmt.Errorf("%q is not a valid domain name", s)
		}
	}
	if len(ascii) > 253 {
		return "", fmt.Errorf("%q is too long", s)
	}
	return registrable(ascii)
}

// registrable reduces name to the part that is registered with a registry
// (www.example.co.uk -> example.co.uk). Registries answer "not found" for
// subdomains, which would otherwise look like an available domain. Only
// ICANN suffixes count: foo.github.io is registered as github.io.
func registrable(name string) (string, error) {
	labels := strings.Split(name, ".")
	suffix := 1
	for n := 2; n <= len(labels); n++ {
		s := strings.Join(labels[len(labels)-n:], ".")
		if ps, icann := publicsuffix.PublicSuffix(s); ps == s && icann {
			suffix = n
		}
	}
	if suffix >= len(labels) {
		return "", fmt.Errorf("%q is a public suffix; enter a domain under it", name)
	}
	return strings.Join(labels[len(labels)-suffix-1:], "."), nil
}

// Display returns name in Unicode, for internationalised domain names.
func Display(name string) string {
	if u, err := idna.Lookup.ToUnicode(name); err == nil {
		return u
	}
	return name
}

func tld(name string) string { return name[strings.LastIndex(name, ".")+1:] }
