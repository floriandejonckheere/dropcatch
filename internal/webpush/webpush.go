// Package webpush sends Web Push notifications (RFC 8030) to browsers,
// with the payload encrypted for the subscription (RFC 8291) and the
// sender identified by a VAPID key (RFC 8292).
//
// It needs no account with a push service: the browser hands out a
// subscription (an endpoint URL and two keys) and accepts messages signed
// with the key whose public half it subscribed with.
package webpush

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// ErrGone means that the subscription expired or was cancelled: messages
// to it will never be delivered again.
var ErrGone = errors.New("the device is no longer subscribed")

// Urgency tells the push service how soon to wake the device (RFC 8030).
const (
	UrgencyNormal = "normal"
	UrgencyHigh   = "high"
)

// Subscription is what the browser's PushManager returns; the keys are
// base64url-encoded.
type Subscription struct {
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh"`
	Auth     string `json:"auth"`
}

// Validate checks that s can be sent to.
func (s Subscription) Validate() error {
	u, err := url.Parse(s.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return errors.New("the push endpoint must be an https URL")
	}
	if k, err := decode(s.P256dh); err != nil || len(k) != 65 || k[0] != 4 {
		return errors.New("invalid p256dh key")
	}
	if a, err := decode(s.Auth); err != nil || len(a) != 16 {
		return errors.New("invalid auth secret")
	}
	return nil
}

type Client struct {
	key *ecdsa.PrivateKey
	// Subject is a contact for the push services (a mailto: or https: URL).
	Subject string
	HTTP    *http.Client
	now     func() time.Time
}

// New returns a client that signs with the VAPID key in file, creating
// the key when the file does not exist.
func New(file string) (*Client, error) {
	key, err := loadOrCreateKey(file)
	if err != nil {
		return nil, err
	}
	return &Client{
		key:     key,
		Subject: "https://github.com/floriandejonckheere/dropcatch",
		HTTP:    &http.Client{Timeout: 30 * time.Second},
		now:     time.Now,
	}, nil
}

// PublicKey returns the VAPID public key, base64url-encoded, which
// browsers need to subscribe (applicationServerKey).
func (c *Client) PublicKey() string {
	pub, _ := c.key.PublicKey.Bytes()
	return base64.RawURLEncoding.EncodeToString(pub)
}

func loadOrCreateKey(file string) (*ecdsa.PrivateKey, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return nil, err
		}
		// Anyone with the key can send notifications to the subscribed devices.
		if err := os.WriteFile(file, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
			return nil, err
		}
		return key, nil
	}
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("%s: not a PEM file", file)
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	key, ok := k.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, fmt.Errorf("%s: not a P-256 key", file)
	}
	return key, nil
}

// Send delivers payload to the subscription. The push service keeps it
// for ttl while the device is offline.
func (c *Client) Send(ctx context.Context, sub Subscription, payload []byte, ttl time.Duration, urgency string) error {
	if err := sub.Validate(); err != nil {
		return err
	}
	body, err := encrypt(sub, payload)
	if err != nil {
		return err
	}
	auth, err := c.vapid(sub.Endpoint)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("TTL", strconv.Itoa(int(ttl.Seconds())))
	req.Header.Set("Urgency", urgency)
	req.Header.Set("Authorization", auth)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	msg, _ := io.ReadAll(io.LimitReader(res.Body, 512))
	switch {
	case res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone:
		return ErrGone
	case res.StatusCode >= 300:
		text := strings.TrimSpace(string(msg))
		if text == "" {
			text = http.StatusText(res.StatusCode)
		}
		return fmt.Errorf("the push service answered %d: %s", res.StatusCode, text)
	}
	return nil
}

// vapid returns the Authorization header: a JWT for the endpoint's
// origin, signed with the VAPID key (RFC 8292).
func (c *Client) vapid(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, _ := json.Marshal(map[string]any{
		"aud": u.Scheme + "://" + u.Host,
		"exp": c.now().Add(12 * time.Hour).Unix(),
		"sub": c.Subject,
	})
	signed := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	hash := sha256.Sum256([]byte(signed))
	r, s, err := ecdsa.Sign(rand.Reader, c.key, hash[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return "vapid t=" + signed + "." + base64.RawURLEncoding.EncodeToString(sig) + ", k=" + c.PublicKey(), nil
}

// recordSize is the record size announced in the header; the payload
// always fits in one record.
const recordSize = 4096

// encrypt encrypts payload for the subscription with a new key and salt,
// as one aes128gcm record (RFC 8291 section 3.4, RFC 8188).
func encrypt(sub Subscription, payload []byte) ([]byte, error) {
	uaKey, _ := decode(sub.P256dh)
	authSecret, _ := decode(sub.Auth)
	uaPublic, err := ecdh.P256().NewPublicKey(uaKey)
	if err != nil {
		return nil, err
	}
	asPrivate, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	asPublic := asPrivate.PublicKey().Bytes()
	secret, err := asPrivate.ECDH(uaPublic)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	cek, nonce, err := keys(secret, authSecret, uaKey, asPublic, salt)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// 0x02 marks the last record; there is no padding.
	plain := append(append([]byte{}, payload...), 2)
	if len(plain)+gcm.Overhead() > recordSize-86 {
		return nil, errors.New("the notification is too long")
	}
	out := make([]byte, 0, 86+len(plain)+gcm.Overhead())
	out = append(out, salt...)
	out = binary.BigEndian.AppendUint32(out, recordSize)
	out = append(out, byte(len(asPublic)))
	out = append(out, asPublic...)
	return gcm.Seal(out, nonce, plain, nil), nil
}

// keys derives the content encryption key and nonce from the shared
// secret (RFC 8291 section 3.3 and RFC 8188 section 2.2).
func keys(secret, authSecret, uaPublic, asPublic, salt []byte) (cek, nonce []byte, err error) {
	info := append(append([]byte("WebPush: info\x00"), uaPublic...), asPublic...)
	prk := hmac.New(sha256.New, authSecret)
	prk.Write(secret)
	ikm, err := hkdf.Expand(sha256.New, prk.Sum(nil), string(info), 32)
	if err != nil {
		return nil, nil, err
	}
	key, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, nil, err
	}
	if cek, err = hkdf.Expand(sha256.New, key, "Content-Encoding: aes128gcm\x00", 16); err != nil {
		return nil, nil, err
	}
	nonce, err = hkdf.Expand(sha256.New, key, "Content-Encoding: nonce\x00", 12)
	return cek, nonce, err
}

// decode accepts base64url with or without padding, as browsers differ.
func decode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}
