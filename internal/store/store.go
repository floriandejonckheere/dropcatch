// Package store keeps Dropcatch's state (monitored domains, notification
// targets and settings) in one JSON file in the data directory.
//
// The state is small, so every change rewrites the whole file: it is
// written to a temporary file and renamed over the old one, which never
// leaves a half-written file behind.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const FileName = "dropcatch.json"

// Domain statuses. A domain is Unknown until its first successful check;
// the others match lookup.Status.
const (
	Unknown    = "unknown"
	Registered = "registered"
	Dropping   = "dropping"
	Available  = "available"
)

// historyLimit is how many status changes are kept per domain.
const historyLimit = 20

var (
	ErrNotFound = errors.New("not found")
	ErrExists   = errors.New("already exists")
)

type State struct {
	Settings Settings `json:"settings"`
	Domains  []Domain `json:"domains"`
	Targets  []Target `json:"targets"`
}

type Settings struct {
	// Interval between checks of each domain, in minutes.
	Interval int `json:"interval"`
	// Timeout of one registry query, in seconds.
	Timeout int `json:"timeout"`
	// NotifyChanges also sends notifications when a domain enters its
	// deletion period, is renewed or is registered again; becoming
	// available is always notified.
	NotifyChanges bool `json:"notifyChanges"`
}

const (
	MinInterval = 5
	MaxInterval = 7 * 24 * 60
	MinTimeout  = 5
	MaxTimeout  = 120
)

func DefaultSettings() Settings {
	return Settings{Interval: 60, Timeout: 20, NotifyChanges: true}
}

func (s Settings) Validate() error {
	if s.Interval < MinInterval || s.Interval > MaxInterval {
		return fmt.Errorf("the check interval must be between %d minutes and %d days", MinInterval, MaxInterval/24/60)
	}
	if s.Timeout < MinTimeout || s.Timeout > MaxTimeout {
		return fmt.Errorf("the timeout must be between %d and %d seconds", MinTimeout, MaxTimeout)
	}
	return nil
}

type Domain struct {
	Name   string    `json:"name"` // lowercase ASCII (punycode)
	Note   string    `json:"note,omitempty"`
	Paused bool      `json:"paused,omitempty"`
	Added  time.Time `json:"added"`

	// Last successful check.
	Status    string     `json:"status"`
	Statuses  []string   `json:"statuses,omitempty"`
	Registrar string     `json:"registrar,omitempty"`
	Created   *time.Time `json:"created,omitempty"`
	Expires   *time.Time `json:"expires,omitempty"`
	Source    string     `json:"source,omitempty"`
	Server    string     `json:"server,omitempty"`
	Changed   *time.Time `json:"changed,omitempty"` // when Status last changed

	// Last attempt, successful or not.
	Checked *time.Time `json:"checked,omitempty"`
	Error   string     `json:"error,omitempty"`

	History []Change `json:"history,omitempty"` // newest first
}

type Change struct {
	Time time.Time `json:"time"`
	From string    `json:"from"`
	To   string    `json:"to"`
}

// SetStatus records a status change in the history.
func (d *Domain) SetStatus(status string, now time.Time) {
	if status == d.Status {
		return
	}
	d.History = append([]Change{{Time: now, From: d.Status, To: status}}, d.History...)
	if len(d.History) > historyLimit {
		d.History = d.History[:historyLimit]
	}
	d.Status, d.Changed = status, &now
}

type Target struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"url"` // Apprise URL
	Enabled bool   `json:"enabled"`

	LastSent  *time.Time `json:"lastSent,omitempty"`
	LastError string     `json:"lastError,omitempty"`
}

// Domain returns the named domain; the pointer refers into s.Domains.
func (s State) Domain(name string) *Domain {
	for i := range s.Domains {
		if s.Domains[i].Name == name {
			return &s.Domains[i]
		}
	}
	return nil
}

// Target returns the target with id; the pointer refers into s.Targets.
func (s State) Target(id string) *Target {
	for i := range s.Targets {
		if s.Targets[i].ID == id {
			return &s.Targets[i]
		}
	}
	return nil
}

// AddDomain adds a domain that has not been checked yet.
func (s *State) AddDomain(name, note string, now time.Time) (*Domain, error) {
	if s.Domain(name) != nil {
		return nil, fmt.Errorf("%s: %w", name, ErrExists)
	}
	s.Domains = append(s.Domains, Domain{Name: name, Note: note, Added: now, Status: Unknown})
	slices.SortFunc(s.Domains, func(a, b Domain) int { return strings.Compare(a.Name, b.Name) })
	return s.Domain(name), nil
}

func (s *State) RemoveDomain(name string) error {
	n := len(s.Domains)
	s.Domains = slices.DeleteFunc(s.Domains, func(d Domain) bool { return d.Name == name })
	if len(s.Domains) == n {
		return ErrNotFound
	}
	return nil
}

// AddTarget adds a target with a new ID.
func (s *State) AddTarget(t Target) *Target {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	t.ID = hex.EncodeToString(b)
	s.Targets = append(s.Targets, t)
	return &s.Targets[len(s.Targets)-1]
}

func (s *State) RemoveTarget(id string) error {
	n := len(s.Targets)
	s.Targets = slices.DeleteFunc(s.Targets, func(t Target) bool { return t.ID == id })
	if len(s.Targets) == n {
		return ErrNotFound
	}
	return nil
}

type Store struct {
	mu    sync.Mutex
	file  string
	state State
}

// Open loads the state from dir, or starts with defaults when there is none.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{file: filepath.Join(dir, FileName), state: State{Settings: DefaultSettings()}}
	data, err := os.ReadFile(s.file)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, s.save(s.state)
	case err != nil:
		return nil, err
	}
	if err := json.Unmarshal(data, &s.state); err != nil {
		return nil, fmt.Errorf("reading %s: %w", s.file, err)
	}
	// Fill in settings added after the file was written.
	def := DefaultSettings()
	if s.state.Settings.Interval == 0 {
		s.state.Settings.Interval = def.Interval
	}
	if s.state.Settings.Timeout == 0 {
		s.state.Settings.Timeout = def.Timeout
	}
	return s, nil
}

// Snapshot returns a copy of the state.
func (s *Store) Snapshot() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.state)
}

// Update applies fn to a copy of the state and saves the result. When fn or
// saving fails, the state is left unchanged.
func (s *Store) Update(fn func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.state)
	if err := fn(&next); err != nil {
		return err
	}
	if err := s.save(next); err != nil {
		return err
	}
	s.state = next
	return nil
}

func (s *Store) save(st State) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	// The file holds notification URLs, which often contain credentials.
	tmp, err := os.CreateTemp(filepath.Dir(s.file), "."+FileName+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(append(data, '\n'))
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), s.file)
	}
	if err != nil {
		return fmt.Errorf("saving %s: %w", s.file, err)
	}
	return nil
}

// clone deep-copies the state; it is small enough for a JSON round trip.
func clone(st State) State {
	data, _ := json.Marshal(st)
	var c State
	_ = json.Unmarshal(data, &c)
	if c.Domains == nil {
		c.Domains = []Domain{}
	}
	if c.Targets == nil {
		c.Targets = []Target{}
	}
	return c
}
