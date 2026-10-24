package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStore(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st := s.Snapshot(); st.Settings != DefaultSettings() || len(st.Domains) != 0 {
		t.Fatalf("new state = %+v", st)
	}

	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	err = s.Update(func(st *State) error {
		for _, n := range []string{"b.com", "a.com"} {
			if _, err := st.AddDomain(n, "", now); err != nil {
				return err
			}
		}
		st.AddTarget(Target{Name: "Phone", URL: "ntfy://topic", Enabled: true})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(st *State) error { _, err := st.AddDomain("a.com", "", now); return err }); !errors.Is(err, ErrExists) {
		t.Fatalf("adding a.com twice: %v", err)
	}

	// A failed update leaves the state unchanged.
	_ = s.Update(func(st *State) error {
		st.Domains = nil
		return errors.New("nope")
	})

	// The state survives a restart, sorted by name, and the file is private.
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	st := s.Snapshot()
	if len(st.Domains) != 2 || st.Domains[0].Name != "a.com" || st.Domains[0].Status != Unknown || len(st.Targets) != 1 || st.Targets[0].ID == "" {
		t.Fatalf("reloaded state = %+v", st)
	}
	if fi, _ := os.Stat(filepath.Join(dir, FileName)); fi.Mode().Perm() != 0o600 {
		t.Fatalf("state file mode = %v", fi.Mode())
	}

	// Snapshots are copies.
	st.Domains[0].Note = "changed"
	if s.Snapshot().Domains[0].Note != "" {
		t.Fatal("snapshot shares memory with the store")
	}

	if err := s.Update(func(st *State) error { return st.RemoveDomain("missing.com") }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removing a missing domain: %v", err)
	}
}

func TestHistory(t *testing.T) {
	d := Domain{Status: Unknown}
	now := time.Now()
	d.SetStatus(Registered, now)
	d.SetStatus(Registered, now)
	if len(d.History) != 1 || d.History[0].From != Unknown || d.History[0].To != Registered {
		t.Fatalf("history = %+v", d.History)
	}
	for i := range historyLimit + 5 {
		d.SetStatus([]string{Available, Registered}[i%2], now.Add(time.Duration(i)*time.Minute))
	}
	if len(d.History) != historyLimit || !d.History[0].Time.After(d.History[1].Time) {
		t.Fatalf("history has %d entries, newest first: %v", len(d.History), d.History[:2])
	}
}

func TestSettingsValidate(t *testing.T) {
	for _, s := range []Settings{{Interval: 1, Timeout: 20}, {Interval: 60, Timeout: 500}, {Interval: MaxInterval + 1, Timeout: 20}} {
		if s.Validate() == nil {
			t.Errorf("%+v is valid", s)
		}
	}
	if err := DefaultSettings().Validate(); err != nil {
		t.Error(err)
	}
}
