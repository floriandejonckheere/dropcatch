package auth

import (
	"net/http"
	"net/http/httptest"
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
	if _, err := a.Login("1.1.1.1", "admin", "wrong", false); err == nil {
		t.Fatal("wrong password accepted")
	}
	if _, err := a.Login("1.1.1.1", "root", "hunter2", false); err == nil {
		t.Fatal("wrong username accepted")
	}
	c, err := a.Login("1.1.1.1", "admin", "hunter2", false)
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
		c, err := a.Login("1.1.1.1", "", "pw", false)
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
	if _, err := a.Login("2.2.2.2", "", "s3cret", false); err != nil {
		t.Fatalf("bcrypt login failed: %v", err)
	}
	for range maxFailures {
		_, _ = a.Login("3.3.3.3", "", "bad", false)
	}
	if _, err := a.Login("3.3.3.3", "", "s3cret", false); err != ErrRateLimited {
		t.Fatalf("expected rate limit, got %v", err)
	}
	if _, err := a.Login("4.4.4.4", "", "s3cret", false); err != nil {
		t.Fatalf("other client limited: %v", err)
	}
}
