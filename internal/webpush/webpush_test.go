package webpush

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func b64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := decode(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// decrypt is the browser's side: it decrypts a message body with the
// subscription's private key.
func decrypt(t *testing.T, body, uaPrivate, authSecret []byte) []byte {
	t.Helper()
	priv, err := ecdh.P256().NewPrivateKey(uaPrivate)
	if err != nil {
		t.Fatal(err)
	}
	salt, idlen := body[:16], int(body[20])
	asPublic, ciphertext := body[21:21+idlen], body[21+idlen:]
	pub, err := ecdh.P256().NewPublicKey(asPublic)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := priv.ECDH(pub)
	if err != nil {
		t.Fatal(err)
	}
	cek, nonce, err := keys(secret, authSecret, priv.PublicKey().Bytes(), asPublic, salt)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plain[len(plain)-1] != 2 {
		t.Fatalf("missing last-record delimiter: %x", plain)
	}
	return plain[:len(plain)-1]
}

// The example in RFC 8291 appendix A checks the key derivation.
func TestRFC8291Example(t *testing.T) {
	body := b64(t, "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPTpK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN")
	got := decrypt(t, body, b64(t, "q1dXpw3UpT5VOmu_cf_v6ih07Aems3njxI-JWgLcM94"), b64(t, "BTBZMqHH6r4Tts7J_aSIgg"))
	if string(got) != "When I grow up, I want to be a watermelon" {
		t.Fatalf("decrypted %q", got)
	}
}

func TestSend(t *testing.T) {
	ua, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)

	var gotBody []byte
	var gotHeader http.Header
	status := http.StatusCreated
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotHeader = r.Header
		w.WriteHeader(status)
	}))
	defer srv.Close()

	file := filepath.Join(t.TempDir(), "vapid.key")
	c, err := New(file)
	if err != nil {
		t.Fatal(err)
	}
	c.HTTP = srv.Client()
	sub := Subscription{
		Endpoint: srv.URL + "/push/abc",
		P256dh:   base64.RawURLEncoding.EncodeToString(ua.PublicKey().Bytes()),
		Auth:     base64.URLEncoding.EncodeToString(auth), // padded, as some browsers send it
	}
	if err := c.Send(context.Background(), sub, []byte(`{"title":"hi"}`), time.Hour, UrgencyHigh); err != nil {
		t.Fatal(err)
	}
	if got := decrypt(t, gotBody, ua.Bytes(), auth); string(got) != `{"title":"hi"}` {
		t.Fatalf("payload %q", got)
	}
	if gotHeader.Get("Content-Encoding") != "aes128gcm" || gotHeader.Get("TTL") != "3600" || gotHeader.Get("Urgency") != "high" {
		t.Fatalf("headers %v", gotHeader)
	}

	// The VAPID token is for the endpoint's origin and signed with the
	// public key it announces.
	token, key, ok := strings.Cut(strings.TrimPrefix(gotHeader.Get("Authorization"), "vapid t="), ", k=")
	if !ok || key != c.PublicKey() {
		t.Fatalf("authorization %q", gotHeader.Get("Authorization"))
	}
	parts := strings.Split(token, ".")
	var claims struct {
		Aud string `json:"aud"`
		Sub string `json:"sub"`
	}
	_ = json.Unmarshal(b64(t, parts[1]), &claims)
	if claims.Aud != srv.URL || claims.Sub == "" {
		t.Fatalf("claims %+v", claims)
	}
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), b64(t, key))
	if err != nil {
		t.Fatal(err)
	}
	sig := b64(t, parts[2])
	hash := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(pub, hash[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("bad VAPID signature")
	}

	// The key is kept, so that subscriptions stay valid.
	again, err := New(file)
	if err != nil || again.PublicKey() != c.PublicKey() {
		t.Fatalf("key not reloaded: %v", err)
	}

	status = http.StatusGone
	if err := c.Send(context.Background(), sub, []byte("x"), time.Hour, UrgencyNormal); !errors.Is(err, ErrGone) {
		t.Fatalf("gone subscription: %v", err)
	}
	status = http.StatusTooManyRequests
	if err := c.Send(context.Background(), sub, []byte("x"), time.Hour, UrgencyNormal); err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("rate limited: %v", err)
	}
	if err := c.Send(context.Background(), sub, bytes.Repeat([]byte("x"), 5000), time.Hour, UrgencyNormal); err == nil {
		t.Fatal("too long payload accepted")
	}
}

func TestValidate(t *testing.T) {
	ua, _ := ecdh.P256().GenerateKey(rand.Reader)
	good := Subscription{Endpoint: "https://push.example/x", P256dh: base64.RawURLEncoding.EncodeToString(ua.PublicKey().Bytes()), Auth: "BTBZMqHH6r4Tts7J_aSIgg"}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, s := range []Subscription{
		{Endpoint: "http://push.example/x", P256dh: good.P256dh, Auth: good.Auth},
		{Endpoint: good.Endpoint, P256dh: "abc", Auth: good.Auth},
		{Endpoint: good.Endpoint, P256dh: good.P256dh, Auth: "abc"},
	} {
		if s.Validate() == nil {
			t.Errorf("%+v is valid", s)
		}
	}
}
