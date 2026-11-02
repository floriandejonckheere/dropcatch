package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testKey  = "0123456789abcdef0123456789abcdef"
	otherKey = "fedcba9876543210fedcba9876543210"
)

func TestEncryptedCredentials(t *testing.T) {
	// A state file from before the secret key, with plain-text credentials.
	dir := t.TempDir()
	file := filepath.Join(dir, FileName)
	plain := `{"settings":{"interval":60,"vpsInterval":15,"stockInterval":2,"timeout":20},
		"targets":[{"id":"t1","name":"Phone","type":"apprise","url":"ntfy://user:pass@topic","enabled":true},{"id":"t2","name":"Browser","type":"push","url":"","push":{"endpoint":"https://push.example/1","p256dh":"k","auth":"a"},"enabled":true}],
		"projects":[{"id":"p1","name":"Prod","token":"hz-secret-token","added":"2026-10-24T12:00:00Z"}]}`
	_ = os.WriteFile(file, []byte(plain), 0o600)

	// Opening it with a key encrypts them in the file, not in memory.
	s, err := Open(dir, testKey)
	if err != nil {
		t.Fatal(err)
	}
	st := s.Snapshot()
	if st.Targets[0].URL != "ntfy://user:pass@topic" || st.Projects[0].Token != "hz-secret-token" || st.HetznerToken() != "hz-secret-token" {
		t.Fatalf("state = %+v", st)
	}
	data, _ := os.ReadFile(file)
	if strings.Contains(string(data), "user:pass") || strings.Contains(string(data), "hz-secret-token") || strings.Count(string(data), encPrefix) != 2 {
		t.Fatalf("credentials not encrypted:\n%s", data)
	}

	// Changes are saved encrypted, and read back.
	_ = s.Update(func(st *State) error {
		st.AddProject(Project{Name: "Staging", Token: "hz-staging"}, st.Projects[0].Added)
		return nil
	})
	if data, _ = os.ReadFile(file); strings.Contains(string(data), "hz-staging") {
		t.Fatalf("new token not encrypted:\n%s", data)
	}
	if s, err = Open(dir, testKey); err != nil {
		t.Fatal(err)
	}
	if st := s.Snapshot(); st.Projects[1].Token != "hz-staging" || st.Targets[0].URL != "ntfy://user:pass@topic" || st.Targets[1].URL != "" {
		t.Fatalf("reopened state = %+v", st)
	}

	// Without the key, or with another, the state cannot be read.
	if _, err := Open(dir, ""); !errors.Is(err, errNoKey) {
		t.Errorf("without key: %v", err)
	}
	if _, err := Open(dir, otherKey); !errors.Is(err, errWrongKey) {
		t.Errorf("with another key: %v", err)
	}
	if _, err := Open(t.TempDir(), "short"); err == nil {
		t.Error("short key accepted")
	}
	// A failed open leaves the file as it was.
	if after, _ := os.ReadFile(file); string(after) != string(data) {
		t.Error("file changed by a failed open")
	}
}

func TestPlainCredentials(t *testing.T) {
	// Without a key, credentials stay readable, as before.
	dir := t.TempDir()
	s, err := Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Update(func(st *State) error {
		st.AddTarget(Target{Name: "Phone", URL: "ntfy://topic"})
		return nil
	})
	if data, _ := os.ReadFile(filepath.Join(dir, FileName)); !strings.Contains(string(data), `"url": "ntfy://topic"`) {
		t.Fatalf("state file:\n%s", data)
	}
	if (State{}).HetznerToken() != "" {
		t.Error("token without projects")
	}
}
