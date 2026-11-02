// Package auth implements single-user password authentication with signed
// session cookies. Sessions are stateless except for logged-out sessions,
// which are remembered until they would have expired.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	CookieName = "dropcatch_session"
	sessionTTL = 30 * 24 * time.Hour

	// Failed logins allowed per client within failWindow. A client is an
	// IPv4 address or an IPv6 /64, which one subscriber usually has whole.
	maxFailures = 10
	failWindow  = 15 * time.Minute
	// maxClients bounds the clients with recent failures that are
	// remembered. When that many are, logins from other clients are
	// refused until some expire: forgetting clients instead would let a
	// flood of addresses reset their own counts.
	maxClients = 10000
)

var ErrRateLimited = errors.New("too many failed login attempts, try again later")

type Auth struct {
	username string
	password string
	hash     []byte
	key      []byte

	mu       sync.Mutex
	failures map[string][]time.Time
	revoked  map[string]int64 // session ID -> expiry (Unix seconds)
	// revokedFile persists revoked sessions across restarts; empty keeps
	// them in memory only.
	revokedFile string
}

// New returns nil when no password is configured (authentication disabled).
// The session key is derived from secret, or from a random key stored in
// dataDir, and from the credentials, so changing the password logs out all
// sessions.
func New(username, password, passwordHash, secret, dataDir string) (*Auth, error) {
	if password == "" && passwordHash == "" {
		return nil, nil
	}
	if secret == "" {
		var err error
		if secret, err = loadOrCreateSecret(filepath.Join(dataDir, "session.key")); err != nil {
			return nil, err
		}
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(username + "\x00" + password + "\x00" + passwordHash))
	a := &Auth{
		username: username,
		password: password,
		hash:     []byte(passwordHash),
		key:      mac.Sum(nil),
		failures: map[string][]time.Time{},
		revoked:  map[string]int64{},
	}
	if dataDir != "" {
		a.revokedFile = filepath.Join(dataDir, "revoked-sessions.json")
		a.loadRevoked()
	}
	return a, nil
}

// RequiresUsername reports whether the login form needs a username field.
func (a *Auth) RequiresUsername() bool { return a.username != "" }

// Login checks credentials for the client at ip, and returns a session cookie.
func (a *Auth) Login(ip netip.Addr, username, password string, secure bool) (*http.Cookie, error) {
	client := clientKey(ip)
	if a.limited(client) {
		return nil, ErrRateLimited
	}
	userOK := subtle.ConstantTimeCompare([]byte(username), []byte(a.username)) == 1
	var passOK bool
	if len(a.hash) > 0 {
		passOK = bcrypt.CompareHashAndPassword(a.hash, []byte(password)) == nil
	} else {
		ph, pw := sha256.Sum256([]byte(password)), sha256.Sum256([]byte(a.password))
		passOK = subtle.ConstantTimeCompare(ph[:], pw[:]) == 1
	}
	if !userOK || !passOK {
		a.fail(client)
		return nil, errors.New("invalid credentials")
	}
	exp := time.Now().Add(sessionTTL)
	return &http.Cookie{
		Name:     CookieName,
		Value:    a.sign(exp, newSessionID()),
		Path:     "/",
		Expires:  exp,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}, nil
}

// Logout returns a cookie that clears the session.
func Logout(secure bool) *http.Cookie {
	return &http.Cookie{Name: CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode}
}

// Valid reports whether r carries a valid, unrevoked session cookie.
func (a *Auth) Valid(r *http.Request) bool {
	id, _, ok := a.session(r)
	if !ok {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	_, revoked := a.revoked[id]
	return !revoked
}

// Revoke invalidates the session carried by r, if any, so that a copy of its
// cookie stops working after logout.
func (a *Auth) Revoke(r *http.Request) {
	id, exp, ok := a.session(r)
	if !ok {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now().Unix()
	for k, e := range a.revoked {
		if e < now {
			delete(a.revoked, k)
		}
	}
	a.revoked[id] = exp
	a.saveRevoked()
}

// session verifies the cookie of r and returns its session ID and expiry.
func (a *Auth) session(r *http.Request) (id string, exp int64, ok bool) {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return "", 0, false
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 3 || parts[1] == "" || parts[2] == "" {
		return "", 0, false
	}
	exp, err = strconv.ParseInt(parts[0], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return "", 0, false
	}
	if !hmac.Equal([]byte(c.Value), []byte(a.sign(time.Unix(exp, 0), parts[1]))) {
		return "", 0, false
	}
	return parts[1], exp, true
}

// sign returns the cookie value for session id expiring at exp:
// "<exp>.<id>.<HMAC of exp and id>".
func (a *Auth) sign(exp time.Time, id string) string {
	s := strconv.FormatInt(exp.Unix(), 10) + "." + id
	mac := hmac.New(sha256.New, a.key)
	mac.Write([]byte(s))
	return s + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func newSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (a *Auth) loadRevoked() {
	data, err := os.ReadFile(a.revokedFile)
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, &a.revoked); err != nil {
		slog.Warn("ignoring unreadable revoked sessions", "file", a.revokedFile, "err", err)
		a.revoked = map[string]int64{}
	}
}

// saveRevoked writes the revoked sessions to disk. The caller holds a.mu.
func (a *Auth) saveRevoked() {
	if a.revokedFile == "" {
		return
	}
	data, _ := json.Marshal(a.revoked)
	tmp := a.revokedFile + ".tmp"
	err := os.WriteFile(tmp, data, 0o600)
	if err == nil {
		err = os.Rename(tmp, a.revokedFile)
	}
	if err != nil {
		slog.Warn("could not save revoked sessions", "err", err)
	}
}

func (a *Auth) limited(client string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, known := a.failures[client]; !known && len(a.failures) >= maxClients {
		a.prune()
		return len(a.failures) >= maxClients
	}
	return a.recent(client) >= maxFailures
}

// recent drops the client's failures that are older than failWindow and
// returns how many are left. The caller holds a.mu.
func (a *Auth) recent(client string) int {
	recent := a.failures[client][:0]
	for _, t := range a.failures[client] {
		if time.Since(t) < failWindow {
			recent = append(recent, t)
		}
	}
	a.failures[client] = recent
	if len(recent) == 0 {
		delete(a.failures, client)
	}
	return len(recent)
}

// prune forgets the clients without recent failures. The caller holds a.mu.
func (a *Auth) prune() {
	for client := range a.failures {
		a.recent(client)
	}
}

func (a *Auth) fail(client string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, known := a.failures[client]; !known && len(a.failures) >= maxClients {
		a.prune()
		if len(a.failures) >= maxClients {
			return
		}
	}
	a.failures[client] = append(a.failures[client], time.Now())
}

// clientKey returns the address the rate limit counts failures by: the
// IPv4 address, or the /64 of an IPv6 address.
func clientKey(ip netip.Addr) string {
	ip = ip.Unmap()
	if ip.Is6() {
		p, _ := ip.Prefix(64)
		return p.String()
	}
	return ip.String()
}

// ClientIP returns the address of the client that sent r. When r comes
// from a trusted proxy, that is the last address in X-Forwarded-For that
// is not a trusted proxy itself: proxies append the address they received
// the request from, so the addresses before it may be forged.
func ClientIP(r *http.Request, trusted []netip.Prefix) netip.Addr {
	peer, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	ip := peer.Addr().Unmap()
	isTrusted := func(a netip.Addr) bool {
		return slices.ContainsFunc(trusted, func(p netip.Prefix) bool { return p.Contains(a) })
	}
	if !isTrusted(ip) {
		return ip
	}
	var hops []string
	for _, h := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(h, ",")...)
	}
	for i := len(hops) - 1; i >= 0; i-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			// Not an address: what comes before it cannot be trusted.
			return ip
		}
		ip = hop.Unmap()
		if !isTrusted(ip) {
			return ip
		}
	}
	return ip
}

func loadOrCreateSecret(file string) (string, error) {
	if b, err := os.ReadFile(file); err == nil && len(b) >= 32 {
		return string(b), nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	s := hex.EncodeToString(b)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(file, []byte(s), 0o600); err != nil {
		return "", err
	}
	return s, nil
}
