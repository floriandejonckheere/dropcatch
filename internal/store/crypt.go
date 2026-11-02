package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

// Credentials (the API tokens of Hetzner projects and Apprise URLs) are
// encrypted in the state file when a secret key is configured, so that the
// file, or a backup of the data directory, does not give them away without
// the key, which is kept outside of it. In memory they are plain text.
//
// An encrypted value is encPrefix followed by the base64 of a random nonce
// and the AES-256-GCM ciphertext. Values without the prefix are plain
// text, as written before a key was set; they are encrypted when the state
// is opened with a key.

const encPrefix = "enc:v1:"

// MinSecretKey is the shortest secret key accepted.
const MinSecretKey = 32

var (
	errNoKey    = errors.New("its credentials are encrypted: set DROPCATCH_SECRET_KEY to the key they were encrypted with")
	errWrongKey = errors.New("could not decrypt its credentials: DROPCATCH_SECRET_KEY is not the key they were encrypted with")
)

// sealer encrypts and decrypts credentials; nil without a secret key,
// when they are stored as plain text.
type sealer struct{ aead cipher.AEAD }

func newSealer(secret string) (*sealer, error) {
	if secret == "" {
		return nil, nil
	}
	if len(secret) < MinSecretKey {
		return nil, errors.New("the secret key must have at least 32 characters")
	}
	// The secret is random text of any length; HKDF turns it into an
	// AES-256 key.
	key, err := hkdf.Key(sha256.New, []byte(secret), nil, "dropcatch credentials v1", 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &sealer{aead}, nil
}

func (s *sealer) seal(plain string) string {
	if s == nil || plain == "" {
		return plain
	}
	nonce := make([]byte, s.aead.NonceSize())
	_, _ = rand.Read(nonce)
	return encPrefix + base64.RawStdEncoding.EncodeToString(s.aead.Seal(nonce, nonce, []byte(plain), nil))
}

func (s *sealer) open(value string) (string, error) {
	enc, ok := strings.CutPrefix(value, encPrefix)
	if !ok {
		return value, nil
	}
	if s == nil {
		return "", errNoKey
	}
	data, err := base64.RawStdEncoding.DecodeString(enc)
	n := s.aead.NonceSize()
	if err != nil || len(data) < n {
		return "", errWrongKey
	}
	plain, err := s.aead.Open(nil, data[:n], data[n:], nil)
	if err != nil {
		return "", errWrongKey
	}
	return string(plain), nil
}

// credentials returns the fields of st that hold credentials.
func credentials(st *State) []*string {
	var out []*string
	for i := range st.Projects {
		out = append(out, &st.Projects[i].Token)
	}
	for i := range st.Targets {
		out = append(out, &st.Targets[i].URL)
	}
	return out
}
