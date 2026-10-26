// Package store keeps Dropcatch's state (monitored domains, notification
// targets and settings) in one JSON file in the data directory.
//
// The state is small, so every change rewrites the whole file: it is
// written to a temporary file and renamed over the old one, which never
// leaves a half-written file behind.
package store

import (
	"cmp"
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

// Statuses. Everything is Unknown until its first successful check.
// Domains are Registered, Dropping or Available (as in lookup.Status);
// VPS watches are OutOfStock or Available.
const (
	Unknown    = "unknown"
	Registered = "registered"
	Dropping   = "dropping"
	Available  = "available"
	OutOfStock = "out-of-stock"
)

// historyLimit is how many status changes are kept per domain or VPS.
const historyLimit = 20

// periodLimit is how many stock periods are kept per VPS.
const periodLimit = 100

var (
	ErrNotFound = errors.New("not found")
	ErrExists   = errors.New("already exists")
)

type State struct {
	Settings Settings `json:"settings"`
	Domains  []Domain `json:"domains"`
	VPS      []VPS    `json:"vps"`
	Targets  []Target `json:"targets"`
}

type Settings struct {
	// Interval between checks of each domain, in minutes. Registrations
	// change slowly, so a few checks a day are enough.
	Interval int `json:"interval"`
	// VPSInterval is the interval between checks of each VPS, in minutes.
	// Restocks can sell out within the hour, so it is much shorter.
	VPSInterval int `json:"vpsInterval"`
	// StockInterval replaces the interval of a VPS while it is in stock
	// (when shorter), so that the end of the stock is seen soon after it
	// happens, in minutes.
	StockInterval int `json:"stockInterval"`
	// Timeout of one registry query, in seconds.
	Timeout int `json:"timeout"`
	// NotifyChanges also sends notifications when a domain enters its
	// deletion period, is renewed or is registered again, and when a VPS
	// sells out again; becoming available is always notified.
	NotifyChanges bool `json:"notifyChanges"`
}

const (
	MinInterval      = 5
	MaxInterval      = 7 * 24 * 60
	MinVPSInterval   = 1
	MaxVPSInterval   = MaxInterval
	MinStockInterval = 1
	MaxStockInterval = 60
	MinTimeout       = 5
	MaxTimeout       = 120
)

func DefaultSettings() Settings {
	return Settings{Interval: 12 * 60, VPSInterval: 15, StockInterval: 2, Timeout: 20, NotifyChanges: true}
}

func (s Settings) Validate() error {
	if err := ValidateDomainInterval(s.Interval); err != nil {
		return err
	}
	if err := ValidateVPSInterval(s.VPSInterval); err != nil {
		return err
	}
	if s.StockInterval < MinStockInterval || s.StockInterval > MaxStockInterval {
		return fmt.Errorf("the interval while in stock must be between %d and %d minutes", MinStockInterval, MaxStockInterval)
	}
	if s.Timeout < MinTimeout || s.Timeout > MaxTimeout {
		return fmt.Errorf("the timeout must be between %d and %d seconds", MinTimeout, MaxTimeout)
	}
	return nil
}

// ValidateDomainInterval checks a domain check interval in minutes.
func ValidateDomainInterval(minutes int) error {
	if minutes < MinInterval || minutes > MaxInterval {
		return fmt.Errorf("the domain check interval must be between %d minutes and %d days", MinInterval, MaxInterval/24/60)
	}
	return nil
}

// ValidateVPSInterval checks a VPS check interval in minutes.
func ValidateVPSInterval(minutes int) error {
	if minutes < MinVPSInterval || minutes > MaxVPSInterval {
		return fmt.Errorf("the VPS check interval must be between %d minute and %d days", MinVPSInterval, MaxVPSInterval/24/60)
	}
	return nil
}

// DomainEvery returns how often d is checked: its own interval, or the
// one of all domains.
func (s Settings) DomainEvery(d Domain) time.Duration {
	return time.Duration(cmp.Or(d.Interval, s.Interval)) * time.Minute
}

// VPSEvery returns how often v is checked: its own interval or the one of
// all VPS, and while it is in stock, at most the in-stock interval.
func (s Settings) VPSEvery(v VPS) time.Duration {
	m := cmp.Or(v.Interval, s.VPSInterval)
	if v.Status == Available {
		m = min(m, s.StockInterval)
	}
	return time.Duration(m) * time.Minute
}

type Domain struct {
	Name   string `json:"name"` // lowercase ASCII (punycode)
	Note   string `json:"note,omitempty"`
	Paused bool   `json:"paused,omitempty"`
	// Interval replaces Settings.Interval for this domain, in minutes;
	// 0 uses the setting.
	Interval int       `json:"interval,omitempty"`
	Added    time.Time `json:"added"`

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
	Time   time.Time `json:"time"`
	From   string    `json:"from"`
	To     string    `json:"to"`
	Detail string    `json:"detail,omitempty"` // such as the datacenters of a VPS
}

// SetStatus records a status change in the history.
func (d *Domain) SetStatus(status string, now time.Time) {
	setStatus(&d.Status, &d.Changed, &d.History, status, "", now)
}

// setStatus changes *status to to, and records the change when it is one,
// or when only its detail changed.
func setStatus(status *string, changed **time.Time, history *[]Change, to, detail string, now time.Time) {
	if to == *status && (len(*history) == 0 || (*history)[0].Detail == detail) {
		return
	}
	*history = append([]Change{{Time: now, From: *status, To: to, Detail: detail}}, *history...)
	if len(*history) > historyLimit {
		*history = (*history)[:historyLimit]
	}
	if to != *status {
		*status, *changed = to, &now
	}
}

// VPS providers.
const (
	OVH     = "ovh"
	Hetzner = "hetzner" // Hetzner Cloud
)

// ProviderName returns the name of a provider as shown to the user.
func ProviderName(provider string) string {
	if provider == Hetzner {
		return "Hetzner Cloud"
	}
	return "OVHcloud"
}

// VPS watches the stock of an OVHcloud VPS plan or a Hetzner Cloud server
// type.
type VPS struct {
	ID         string `json:"id"`
	Provider   string `json:"provider"`
	Subsidiary string `json:"subsidiary,omitempty"` // OVH country shop, such as FR
	Plan       string `json:"plan"`                 // plan code, such as vps-2025-model1 or cx23
	PlanName   string `json:"planName"`
	// Datacenters (OVH, such as eu-west-gra) or locations (Hetzner, such
	// as fsn1) to watch; empty for any.
	Datacenters []string `json:"datacenters"`
	Note        string   `json:"note,omitempty"`
	Paused      bool     `json:"paused,omitempty"`
	// Interval replaces Settings.VPSInterval for this watch, in minutes;
	// 0 uses the setting.
	Interval int       `json:"interval,omitempty"`
	Added    time.Time `json:"added"`

	// Last successful check: Available when a watched datacenter has stock.
	Status  string     `json:"status"`
	Stock   []Stock    `json:"stock,omitempty"` // every datacenter that offers the plan
	Changed *time.Time `json:"changed,omitempty"`

	// Last attempt, successful or not.
	Checked *time.Time `json:"checked,omitempty"`
	Error   string     `json:"error,omitempty"`

	History []Change `json:"history,omitempty"` // newest first
	// When each datacenter had stock, watched or not, newest first.
	Periods []Period `json:"periods,omitempty"`
}

// Period is a time a datacenter had stock. It starts at the first check
// that saw the stock and ends at the first that did not (End is nil while
// it lasts), so its precision is the interval between checks.
type Period struct {
	Code  string     `json:"code"`
	Name  string     `json:"name"`
	Start time.Time  `json:"start"`
	End   *time.Time `json:"end,omitempty"`
}

type Stock struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Region    string `json:"region"`
	Available bool   `json:"available"`
}

// Watches reports whether v watches the datacenter code.
func (v *VPS) Watches(code string) bool {
	return len(v.Datacenters) == 0 || slices.Contains(v.Datacenters, code)
}

// InStock returns the watched datacenters with stock.
func (v *VPS) InStock() []Stock {
	var out []Stock
	for _, s := range v.Stock {
		if s.Available && v.Watches(s.Code) {
			out = append(out, s)
		}
	}
	return out
}

// SetStock records a check result. The history also notes when the
// datacenters with stock change while the status stays Available.
func (v *VPS) SetStock(stock []Stock, now time.Time) {
	v.Stock = stock
	v.setPeriods(now)
	status, names := OutOfStock, []string{}
	for _, s := range v.InStock() {
		status = Available
		names = append(names, s.Name)
	}
	setStatus(&v.Status, &v.Changed, &v.History, status, strings.Join(names, "; "), now)
}

// setPeriods ends the periods of the datacenters that lost their stock and
// starts one for each that gained it.
func (v *VPS) setPeriods(now time.Time) {
	open := map[string]bool{}
	for i := range v.Periods {
		p := &v.Periods[i]
		if p.End != nil {
			continue
		}
		if slices.ContainsFunc(v.Stock, func(s Stock) bool { return s.Code == p.Code && s.Available }) {
			open[p.Code] = true
		} else {
			p.End = &now
		}
	}
	for _, s := range v.Stock {
		if s.Available && !open[s.Code] {
			v.Periods = append([]Period{{Code: s.Code, Name: s.Name, Start: now}}, v.Periods...)
		}
	}
	if len(v.Periods) > periodLimit {
		v.Periods = v.Periods[:periodLimit]
	}
}

// Target types.
const (
	Apprise = "apprise"
	Push    = "push" // Web Push to a browser or installed app
)

type Target struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	Type    string        `json:"type"`
	URL     string        `json:"url"`            // Apprise URL
	Push    *Subscription `json:"push,omitempty"` // Web Push subscription
	Enabled bool          `json:"enabled"`

	LastSent  *time.Time `json:"lastSent,omitempty"`
	LastError string     `json:"lastError,omitempty"`
}

// Subscription is a browser's Web Push subscription; the keys are
// base64url-encoded.
type Subscription struct {
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh"`
	Auth     string `json:"auth"`
}

// PushTarget returns the target with the push endpoint, or nil.
func (s State) PushTarget(endpoint string) *Target {
	for i := range s.Targets {
		if t := &s.Targets[i]; t.Push != nil && t.Push.Endpoint == endpoint {
			return t
		}
	}
	return nil
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

// VPSWatch returns the VPS watch with id; the pointer refers into s.VPS.
func (s State) VPSWatch(id string) *VPS {
	for i := range s.VPS {
		if s.VPS[i].ID == id {
			return &s.VPS[i]
		}
	}
	return nil
}

// AddVPS adds a VPS watch with a new ID, unless an identical one exists.
func (s *State) AddVPS(v VPS, now time.Time) (*VPS, error) {
	for _, o := range s.VPS {
		if o.Provider == v.Provider && o.Subsidiary == v.Subsidiary && o.Plan == v.Plan && slices.Equal(o.Datacenters, v.Datacenters) {
			return nil, fmt.Errorf("%s in these datacenters is already watched: %w", v.PlanName, ErrExists)
		}
	}
	v.ID, v.Added, v.Status = newID(), now, Unknown
	if v.Provider == "" {
		v.Provider = OVH
	}
	s.VPS = append(s.VPS, v)
	return &s.VPS[len(s.VPS)-1], nil
}

func (s *State) RemoveVPS(id string) error {
	n := len(s.VPS)
	s.VPS = slices.DeleteFunc(s.VPS, func(v VPS) bool { return v.ID == id })
	if len(s.VPS) == n {
		return ErrNotFound
	}
	return nil
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// AddTarget adds a target with a new ID.
func (s *State) AddTarget(t Target) *Target {
	t.ID = newID()
	if t.Type == "" {
		t.Type = Apprise
	}
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
	// The intervals are filled in below, depending on which the file has.
	s.state.Settings.Interval, s.state.Settings.VPSInterval = 0, 0
	if err := json.Unmarshal(data, &s.state); err != nil {
		return nil, fmt.Errorf("reading %s: %w", s.file, err)
	}
	// Fill in settings added after the file was written.
	def := DefaultSettings()
	// Domains and VPS shared one interval before VPSInterval existed:
	// keep checking VPS as often as before.
	if s.state.Settings.VPSInterval == 0 {
		s.state.Settings.VPSInterval = cmp.Or(s.state.Settings.Interval, def.VPSInterval)
	}
	if s.state.Settings.Interval == 0 {
		s.state.Settings.Interval = def.Interval
	}
	if s.state.Settings.StockInterval == 0 {
		s.state.Settings.StockInterval = def.StockInterval
	}
	// VPS watches were all OVH before Hetzner was added.
	for i := range s.state.VPS {
		if s.state.VPS[i].Provider == "" {
			s.state.VPS[i].Provider = OVH
		}
	}
	// Targets were all Apprise URLs before push was added.
	for i := range s.state.Targets {
		if s.state.Targets[i].Type == "" {
			s.state.Targets[i].Type = Apprise
		}
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
	if c.VPS == nil {
		c.VPS = []VPS{}
	}
	if c.Targets == nil {
		c.Targets = []Target{}
	}
	return c
}
