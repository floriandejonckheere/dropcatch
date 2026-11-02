package auth

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestDisabled(t *testing.T) {
	a, err := New("", "", "", "", t.TempDir())
	if err != nil || a != nil {
		t.Fatalf("New without password = %v, %v", a, err)
	}
}

func TestLoginAndSession(t *testing.T) {
	a, err := New("admin", "hunter2", "", "same-secret", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Login(netip.MustParseAddr("1.1.1.1"), "admin", "wrong", false); err == nil {
		t.Fatal("wrong password accepted")
	}
	if _, err := a.Login(netip.MustParseAddr("1.1.1.1"), "root", "hunter2", false); err == nil {
		t.Fatal("wrong username accepted")
	}
	c, err := a.Login(netip.MustParseAddr("1.1.1.1"), "admin", "hunter2", false)
	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest("GET", "/", nil)
	if a.Valid(r) {
		t.Fatal("request without cookie is valid")
	}
	r.AddCookie(c)
	if !a.Valid(r) {
		t.Fatal("session cookie rejected")
	}

	tampered := httptest.NewRequest("GET", "/", nil)
	tampered.AddCookie(&http.Cookie{Name: CookieName, Value: c.Value[:len(c.Value)-2] + "xx"})
	if a.Valid(tampered) {
		t.Fatal("tampered cookie accepted")
	}

	expired := httptest.NewRequest("GET", "/", nil)
	expired.AddCookie(&http.Cookie{Name: CookieName, Value: a.sign(time.Now().Add(-time.Minute), "id")})
	if a.Valid(expired) {
		t.Fatal("expired cookie accepted")
	}

	// Changing the password invalidates existing sessions.
	b, _ := New("admin", "new", "", "same-secret", "")
	if b.Valid(r) {
		t.Fatal("session survived password change")
	}
}

func TestRevoke(t *testing.T) {
	dir := t.TempDir()
	a, _ := New("", "pw", "", "secret", dir)
	login := func() *http.Request {
		c, err := a.Login(netip.MustParseAddr("1.1.1.1"), "", "pw", false)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("GET", "/", nil)
		r.AddCookie(c)
		return r
	}
	first, second := login(), login()

	a.Revoke(first)
	if a.Valid(first) {
		t.Fatal("revoked session still valid")
	}
	if !a.Valid(second) {
		t.Fatal("revoking one session revoked another")
	}

	// Revocations survive a restart.
	b, _ := New("", "pw", "", "secret", dir)
	if b.Valid(first) || !b.Valid(second) {
		t.Fatalf("after restart: first valid = %v, second valid = %v", b.Valid(first), b.Valid(second))
	}
}

func TestBcryptAndRateLimit(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("s3cret"), bcrypt.MinCost)
	a, err := New("", "", string(hash), "fixed-secret", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Login(netip.MustParseAddr("2.2.2.2"), "", "s3cret", false); err != nil {
		t.Fatalf("bcrypt login failed: %v", err)
	}
	for range maxFailures {
		_, _ = a.Login(netip.MustParseAddr("3.3.3.3"), "", "bad", false)
	}
	if _, err := a.Login(netip.MustParseAddr("3.3.3.3"), "", "s3cret", false); err != ErrRateLimited {
		t.Fatalf("expected rate limit, got %v", err)
	}
	if _, err := a.Login(netip.MustParseAddr("4.4.4.4"), "", "s3cret", false); err != nil {
		t.Fatalf("other client limited: %v", err)
	}
}

func TestRateLimitClients(t *testing.T) {
	a, err := New("", "s3cret", "", "fixed-secret", "")
	if err != nil {
		t.Fatal(err)
	}
	// The addresses of one IPv6 /64 are one client.
	for i := range maxFailures {
		_, _ = a.Login(netip.MustParseAddr("2001:db8:1:2::"+strconv.Itoa(i+1)), "", "bad", false)
	}
	if _, err := a.Login(netip.MustParseAddr("2001:db8:1:2:ffff::1"), "", "s3cret", false); err != ErrRateLimited {
		t.Fatalf("same /64: %v", err)
	}
	if _, err := a.Login(netip.MustParseAddr("2001:db8:1:3::1"), "", "s3cret", false); err != nil {
		t.Fatalf("other /64 limited: %v", err)
	}
	// An IPv4 address is the same client in its IPv6-mapped form.
	for range maxFailures {
		_, _ = a.Login(netip.MustParseAddr("::ffff:192.0.2.1"), "", "bad", false)
	}
	if _, err := a.Login(netip.MustParseAddr("192.0.2.1"), "", "s3cret", false); err != ErrRateLimited {
		t.Fatalf("mapped address: %v", err)
	}

	// When too many clients have failed, new ones are refused rather than
	// the remembered ones forgotten, until their failures expire.
	b, _ := New("", "s3cret", "", "fixed-secret", "")
	for i := range maxClients {
		b.failures[strconv.Itoa(i)] = []time.Time{time.Now()}
	}
	if _, err := b.Login(netip.MustParseAddr("198.51.100.1"), "", "s3cret", false); err != ErrRateLimited {
		t.Fatalf("new client while full: %v", err)
	}
	b.failures["0"] = []time.Time{time.Now().Add(-failWindow)}
	if _, err := b.Login(netip.MustParseAddr("198.51.100.1"), "", "s3cret", false); err != nil {
		t.Fatalf("new client after an expiry: %v", err)
	}
}

func TestClientIP(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("10.0.0.1/32")}
	for _, c := range []struct {
		remote, xff, want string
	}{
		// Not from a proxy: the header is ignored.
		{"203.0.113.5:4000", "198.51.100.7", "203.0.113.5"},
		// From the proxy: the address it appended.
		{"172.18.0.2:4000", "198.51.100.7", "198.51.100.7"},
		// Forged addresses before it do not count.
		{"172.18.0.2:4000", "1.2.3.4, 198.51.100.7", "198.51.100.7"},
		// Through two proxies.
		{"172.18.0.2:4000", "198.51.100.7, 10.0.0.1", "198.51.100.7"},
		// No header, or garbage: the proxy itself.
		{"172.18.0.2:4000", "", "172.18.0.2"},
		{"172.18.0.2:4000", "unknown", "172.18.0.2"},
		{"[::ffff:172.18.0.2]:4000", "2001:db8::1", "2001:db8::1"},
	} {
		r := httptest.NewRequest("POST", "/api/login", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := ClientIP(r, trusted); got.String() != c.want {
			t.Errorf("%s with %q: %v, want %s", c.remote, c.xff, got, c.want)
		}
	}
}
