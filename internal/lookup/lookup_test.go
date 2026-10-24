package lookup

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNormalize(t *testing.T) {
	good := map[string]string{
		"Example.COM":                 "example.com",
		" example.com. ":              "example.com",
		"https://www.example.com/a?b": "example.com",
		"shop.example.co.uk":          "example.co.uk",
		"bücher.de":                   "xn--bcher-kva.de",
		"xn--bcher-kva.de":            "xn--bcher-kva.de",
		"foo.github.io":               "github.io",
		"example.unknowntld":          "example.unknowntld",
	}
	for in, want := range good {
		if got, err := Normalize(in); err != nil || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "com", "co.uk", "192.0.2.1", "exa mple.com", "-example.com", "example..com", strings.Repeat("a", 64) + ".com"} {
		if got, err := Normalize(in); err == nil {
			t.Errorf("Normalize(%q) = %q, want an error", in, got)
		}
	}
	if d := Display("xn--bcher-kva.de"); d != "bücher.de" {
		t.Errorf("Display = %q", d)
	}
}

// rdapServer serves a bootstrap file for .com and .co.uk, which lists an
// HTTP URL first to check that HTTPS is preferred, and domain records:
// taken.com is registered, dropping.com is pending delete, others are free.
func rdapServer(t *testing.T) (*httptest.Server, *int) {
	queries := 0
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/dns.json":
			fmt.Fprintf(w, `{"version":"1.0","services":[
				[["com","net"],["http://insecure.invalid/","%[1]s/com/"]],
				[["co.uk"],["%[1]s/uk/"]]]}`, srv.URL)
			return
		case "/com/domain/taken.com":
			fmt.Fprint(w, `{"objectClassName":"domain","ldhName":"TAKEN.COM",
				"status":["client transfer prohibited","active"],
				"events":[{"eventAction":"registration","eventDate":"2001-02-03T04:05:06Z"},
					{"eventAction":"expiration","eventDate":"2027-02-03T04:05:06Z"}],
				"entities":[{"roles":["registrar"],"vcardArray":["vcard",[["version",{},"text","4.0"],["fn",{},"text","Example Registrar, Inc."]]]}]}`)
		case "/com/domain/dropping.com":
			fmt.Fprint(w, `{"status":["pending delete","redemption period"]}`)
		case "/com/domain/limited.com":
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			http.NotFound(w, r)
		}
		queries++
	}))
	t.Cleanup(srv.Close)
	return srv, &queries
}

func TestRDAP(t *testing.T) {
	srv, queries := rdapServer(t)
	c := New("test")
	c.HTTP = srv.Client()
	c.BootstrapURL = srv.URL + "/dns.json"
	ctx := context.Background()

	r, err := c.Lookup(ctx, "taken.com")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Registered || r.Source != "rdap" || r.Registrar != "Example Registrar, Inc." ||
		r.Expires.Year() != 2027 || r.Created.Year() != 2001 || len(r.Statuses) != 2 {
		t.Fatalf("taken.com = %+v", r)
	}
	if r, err = c.Lookup(ctx, "dropping.com"); err != nil || r.Status != Dropping {
		t.Fatalf("dropping.com = %+v, %v", r, err)
	}
	if r, err = c.Lookup(ctx, "free.com"); err != nil || r.Status != Available {
		t.Fatalf("free.com = %+v, %v", r, err)
	}
	// The longest suffix in the bootstrap file wins.
	if r, err = c.Lookup(ctx, "free.co.uk"); err != nil || r.Status != Available {
		t.Fatalf("free.co.uk = %+v, %v", r, err)
	}
	if _, err = c.Lookup(ctx, "limited.com"); err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("limited.com: %v", err)
	}
	if *queries != 5 {
		t.Fatalf("%d domain queries, want 5 (the bootstrap file is cached)", *queries)
	}
}

// fakeWhois answers WHOIS queries with answers[query].
func fakeWhois(t *testing.T, answers map[string]string) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				q, _ := bufio.NewReader(conn).ReadString('\n')
				fmt.Fprint(conn, answers[strings.TrimSpace(q)])
			}()
		}
	}()
	return l.Addr().String()
}

func TestWhoisFallback(t *testing.T) {
	srv, _ := rdapServer(t)
	registry := fakeWhois(t, map[string]string{
		"taken.be": "Domain:\ttaken.be\nStatus:\tNOT AVAILABLE\nRegistered:\tTue Mar 6 2001\n\nRegistrar:\n\tName:\tExample BV\n\tWebsite: https://example.be\n",
		"free.be":  "% .be Whois Server 6.1\n%\nDomain:\tfree.be\nStatus:\tAVAILABLE\n",
		"gone.be":  "Domain:\tgone.be\nStatus:\tQUARANTINE\n",
	})
	_, port, _ := net.SplitHostPort(registry)
	iana := fakeWhois(t, map[string]string{
		"be": "domain:       BE\n\nwhois:        localhost\n\nstatus:       ACTIVE\n",
		"zz": "domain:       ZZ\n\nstatus:       ACTIVE\n",
	})

	c := New("test")
	c.HTTP = srv.Client()
	c.BootstrapURL = srv.URL + "/dns.json"
	c.IANAWhois = iana
	d := &net.Dialer{}
	c.Dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		// The registry's WHOIS server runs on a random port, not 43.
		if strings.HasPrefix(addr, "localhost:") {
			addr = "127.0.0.1:" + port
		}
		return d.DialContext(ctx, network, addr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r, err := c.Lookup(ctx, "taken.be")
	if err != nil || r.Status != Registered || r.Source != "whois" || r.Registrar != "Example BV" || r.Created.Year() != 2001 {
		t.Fatalf("taken.be = %+v, %v", r, err)
	}
	if r, err = c.Lookup(ctx, "free.be"); err != nil || r.Status != Available {
		t.Fatalf("free.be = %+v, %v", r, err)
	}
	if r, err = c.Lookup(ctx, "gone.be"); err != nil || r.Status != Dropping {
		t.Fatalf("gone.be = %+v, %v", r, err)
	}
	if _, err = c.Lookup(ctx, "example.zz"); err != ErrNoServer {
		t.Fatalf("example.zz: %v", err)
	}
}

func TestParseWhois(t *testing.T) {
	gtld := `Domain Name: EXAMPLE.ORG
Registry Domain ID: 1234
Updated Date: 2026-01-01T00:00:00Z
Creation Date: 1995-08-31T04:00:00Z
Registry Expiry Date: 2027-08-30T04:00:00Z
Registrar: Example Registrar LLC
Domain Status: clientTransferProhibited https://icann.org/epp#clientTransferProhibited
Domain Status: redemptionPeriod https://icann.org/epp#redemptionPeriod

>>> Last update of WHOIS database: 2026-10-24T00:00:00Z <<<

NOTICE: The expiration date displayed in this record ... not found in the registry ...
`
	r, err := parseWhois(gtld)
	if err != nil || r.Status != Dropping || r.Registrar != "Example Registrar LLC" ||
		r.Expires != time.Date(2027, 8, 30, 4, 0, 0, 0, time.UTC) || r.Created.Year() != 1995 {
		t.Fatalf("gTLD record = %+v, %v", r, err)
	}

	free := []string{
		"No match for \"EXAMPLE.COM\".\n>>> Last update of whois database <<<\n",
		"%% NOT FOUND\n",
		"Domain: example.de\nStatus: free\n",
		"The queried object does not exist: \nNo entries found.\n",
		"No Data Found\n",
		"Domain example.nl is free.\n",
	}
	for _, text := range free {
		if r, err := parseWhois(text); err != nil || r.Status != Available {
			t.Errorf("parseWhois(%q) = %+v, %v; want available", text, r, err)
		}
	}

	if _, err := parseWhois("Query rate limit exceeded. Try again later.\n"); err == nil {
		t.Error("rate limit notice parsed as a record")
	}
	if _, err := parseWhois("  \n"); err == nil {
		t.Error("empty response parsed as a record")
	}
}

func TestParseDate(t *testing.T) {
	want := time.Date(2027, 3, 4, 0, 0, 0, 0, time.UTC)
	for _, s := range []string{"2027-03-04", "2027-03-04 (YYYY-MM-DD)", "04-Mar-2027", "Thu Mar 4 2027", "04.03.2027", "2027.03.04 UTC"} {
		if got := parseDate(s); !got.Equal(want) {
			t.Errorf("parseDate(%q) = %v", s, got)
		}
	}
	if got := parseDate("soon"); !got.IsZero() {
		t.Errorf("parseDate(soon) = %v", got)
	}
}
