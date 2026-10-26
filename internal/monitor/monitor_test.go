package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/floriandejonckheere/dropcatch/internal/hetzner"
	"github.com/floriandejonckheere/dropcatch/internal/lookup"
	"github.com/floriandejonckheere/dropcatch/internal/ovh"
	"github.com/floriandejonckheere/dropcatch/internal/store"
	"github.com/floriandejonckheere/dropcatch/internal/webpush"
)

type fakeLookup struct {
	mu      sync.Mutex
	results map[string]lookup.Result
	errs    map[string]error
	calls   []string
}

func (f *fakeLookup) Lookup(_ context.Context, name string) (lookup.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
	return f.results[name], f.errs[name]
}

func (f *fakeLookup) set(name string, status lookup.Status, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.results[name] = lookup.Result{Status: status, Source: "rdap", Statuses: []string{"pending delete"}}
	f.errs[name] = err
}

type fakeOVH struct {
	mu    sync.Mutex
	stock map[string][]ovh.Datacenter // by plan
	errs  map[string]error
}

func (f *fakeOVH) Stock(_ context.Context, subsidiary, plan string) ([]ovh.Datacenter, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stock[plan], f.errs[plan]
}

// set gives plan stock in the datacenters marked with a "+" prefix.
func (f *fakeOVH) set(plan string, err error, codes ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var dcs []ovh.Datacenter
	for _, c := range codes {
		code, avail := strings.CutPrefix(c, "+")
		dcs = append(dcs, ovh.Datacenter{Code: code, Name: ovh.DatacenterName(code), Region: ovh.RegionOf(code), Available: avail})
	}
	f.stock[plan], f.errs[plan] = dcs, err
}

type sent struct{ url, title, body, kind string }

type fakeNotifier struct {
	mu   sync.Mutex
	sent []sent
}

func (f *fakeNotifier) Send(_ context.Context, url, title, body, kind string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sent{url, title, body, kind})
	if strings.HasPrefix(url, "bad://") {
		return errors.New("unreachable")
	}
	return nil
}

func (f *fakeNotifier) take() []sent {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.sent
	f.sent = nil
	return s
}

func setup(t *testing.T, domains ...string) (*Monitor, *store.Store, *fakeLookup, *fakeNotifier) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Update(func(s *store.State) error {
		for _, d := range domains {
			_, _ = s.AddDomain(d, "", time.Now())
		}
		s.AddTarget(store.Target{Name: "ok", URL: "ntfy://ok", Enabled: true})
		s.AddTarget(store.Target{Name: "bad", URL: "bad://x", Enabled: true})
		s.AddTarget(store.Target{Name: "off", URL: "ntfy://off", Enabled: false})
		return nil
	})
	l := &fakeLookup{results: map[string]lookup.Result{}, errs: map[string]error{}}
	n := &fakeNotifier{}
	m := New(st, l, &fakeOVH{stock: map[string][]ovh.Datacenter{}, errs: map[string]error{}}, n)
	m.Pause = time.Millisecond
	return m, st, l, n
}

func TestTransitions(t *testing.T) {
	m, st, l, n := setup(t, "example.com")
	ctx := context.Background()
	steps := []struct {
		status lookup.Status
		err    error
		titles []string // per enabled target
	}{
		{lookup.Registered, nil, nil}, // first check: nothing to report
		{lookup.Registered, nil, nil},
		{lookup.Dropping, nil, []string{"example.com is about to be deleted"}},
		{"", errors.New("timeout"), nil}, // errors keep the status
		{lookup.Available, nil, []string{"example.com is available"}},
		{lookup.Available, nil, nil},
		{lookup.Registered, nil, []string{"example.com was registered"}},
	}
	for i, s := range steps {
		l.set("example.com", s.status, s.err)
		m.check(ctx, "example.com")
		got := n.take()
		var titles []string
		for j, msg := range got {
			if j%2 == 0 {
				titles = append(titles, msg.title)
			}
			if msg.url == "ntfy://off" {
				t.Errorf("step %d: notified a disabled target", i)
			}
		}
		if len(got) != 2*len(s.titles) || strings.Join(titles, "|") != strings.Join(s.titles, "|") {
			t.Errorf("step %d: sent %+v, want %v", i, got, s.titles)
		}
	}

	d := st.Snapshot().Domain("example.com")
	if d.Status != store.Registered || d.Error != "" || d.Checked == nil || len(d.History) != 4 {
		t.Fatalf("domain = %+v", d)
	}
	for _, tg := range st.Snapshot().Targets {
		if (tg.Name == "bad") != (tg.LastError != "") || (tg.Name == "off") != (tg.LastSent == nil) {
			t.Errorf("target %s: lastSent %v, lastError %q", tg.Name, tg.LastSent, tg.LastError)
		}
	}
}

func TestQuietChanges(t *testing.T) {
	m, st, l, n := setup(t, "example.com")
	_ = st.Update(func(s *store.State) error { s.Settings.NotifyChanges = false; return nil })
	ctx := context.Background()
	for _, s := range []lookup.Status{lookup.Registered, lookup.Dropping, lookup.Available, lookup.Registered} {
		l.set("example.com", s, nil)
		m.check(ctx, "example.com")
	}
	if got := n.take(); len(got) != 2 || got[0].title != "example.com is available" {
		t.Fatalf("sent %+v, want only the availability notification", got)
	}

	// A domain that is available on its first check is reported too.
	_ = st.Update(func(s *store.State) error { _, err := s.AddDomain("xn--bcher-kva.de", "", time.Now()); return err })
	l.set("xn--bcher-kva.de", lookup.Available, nil)
	m.check(ctx, "xn--bcher-kva.de")
	if got := n.take(); len(got) != 2 || got[0].title != "bücher.de (xn--bcher-kva.de) is available" {
		t.Fatalf("sent %+v", got)
	}
}

type fakePush struct {
	payloads  []map[string]string
	urgencies []string
}

func (f *fakePush) Send(_ context.Context, _ webpush.Subscription, payload []byte, _ time.Duration, urgency string) error {
	var p map[string]string
	_ = json.Unmarshal(payload, &p)
	f.payloads, f.urgencies = append(f.payloads, p), append(f.urgencies, urgency)
	return nil
}

func TestPush(t *testing.T) {
	m, st, l, _ := setup(t, "drop.com")
	p := &fakePush{}
	m.Push = p
	_ = st.Update(func(s *store.State) error {
		s.AddTarget(store.Target{Name: "phone", Type: store.Push, Push: &store.Subscription{Endpoint: "https://push.example/x"}, Enabled: true})
		return nil
	})
	ctx := context.Background()
	l.set("drop.com", lookup.Registered, nil)
	m.check(ctx, "drop.com")
	l.set("drop.com", lookup.Dropping, nil)
	m.check(ctx, "drop.com")
	l.set("drop.com", lookup.Available, nil)
	m.check(ctx, "drop.com")
	if len(p.payloads) != 2 || p.urgencies[0] != "normal" || p.urgencies[1] != "high" {
		t.Fatalf("pushed %v with %v", p.payloads, p.urgencies)
	}
	if got := p.payloads[1]; got["title"] != "drop.com is available" || got["view"] != "domains" || got["tag"] != "drop.com" {
		t.Fatalf("payload %v", got)
	}
}

type fakeHetzner struct{ locs []hetzner.Location }

func (f *fakeHetzner) Stock(_ context.Context, plan string) ([]hetzner.Location, error) {
	return f.locs, nil
}

func TestHetzner(t *testing.T) {
	m, st, _, n := setup(t)
	var v *store.VPS
	_ = st.Update(func(s *store.State) error {
		v, _ = s.AddVPS(store.VPS{Provider: store.Hetzner, Plan: "cax11", PlanName: "CAX11", Datacenters: []string{"fsn1", "nbg1"}}, time.Now())
		return nil
	})
	ctx := context.Background()

	// Without a token, the check fails and nothing is sent.
	m.check(ctx, VPSKey(v.ID))
	if w := st.Snapshot().VPSWatch(v.ID); w.Status != store.Unknown || !strings.Contains(w.Error, "API token") || len(n.take()) != 0 {
		t.Fatalf("without token: %+v", w)
	}

	h := &fakeHetzner{locs: []hetzner.Location{
		{Code: "fsn1", Name: "Falkenstein, Germany", Region: "europe"},
		{Code: "hel1", Name: "Helsinki, Finland", Region: "europe", Available: true},
	}}
	m.Hetzner = h
	m.check(ctx, VPSKey(v.ID))
	if w := st.Snapshot().VPSWatch(v.ID); w.Status != store.OutOfStock || w.Error != "" || len(n.take()) != 0 {
		t.Fatalf("out of stock in the watched locations: %+v", w)
	}
	h.locs[0].Available = true
	m.check(ctx, VPSKey(v.ID))
	got := n.take()
	if len(got) != 2 || got[0].title != "CAX11 is available" || !strings.Contains(got[0].body, "Hetzner Cloud CAX11 (cax11) can be ordered in Falkenstein, Germany: https://console.hetzner.com/") {
		t.Fatalf("sent %+v", got)
	}
}

func TestSchedule(t *testing.T) {
	m, st, l, _ := setup(t, "a.com", "b.com", "c.com")
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	recent, old := now.Add(-10*time.Minute), now.Add(-50*time.Minute)
	_ = st.Update(func(s *store.State) error {
		s.Settings.Interval = 60
		s.Domain("a.com").Checked = &recent
		s.Domain("b.com").Checked = &old
		s.Domain("c.com").Paused = true
		return nil
	})
	if name, wait := m.next(); name != "" || wait != 10*time.Minute {
		t.Fatalf("next = %q, %v; want b.com in 10 minutes", name, wait)
	}
	now = now.Add(10 * time.Minute)
	if name, _ := m.next(); name != "b.com" {
		t.Fatalf("next = %q, want b.com", name)
	}

	// Requested checks go first, including paused domains, and removed
	// domains are skipped.
	m.Check("c.com", "gone.com", "a.com")
	if got := m.Checking(); strings.Join(got, ",") != "c.com,gone.com,a.com" {
		t.Fatalf("checking = %v", got)
	}
	for _, want := range []string{"c.com", "a.com", "b.com"} {
		if name, _ := m.next(); name != want {
			t.Fatalf("next = %q, want %q", name, want)
		}
	}

	// Run checks the due domains.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.now = time.Now
	_ = st.Update(func(s *store.State) error {
		s.Domain("a.com").Checked, s.Domain("b.com").Checked = nil, nil
		return nil
	})
	l.set("a.com", lookup.Registered, nil)
	l.set("b.com", lookup.Available, nil)
	done := make(chan struct{})
	go func() {
		m.Run(ctx)
		close(done)
	}()
	// Run may be saving a check result: let it finish before the data
	// directory is removed.
	defer func() {
		cancel()
		<-done
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s := st.Snapshot()
		if s.Domain("a.com").Status == store.Registered && s.Domain("b.com").Status == store.Available {
			if s.Domain("c.com").Checked != nil {
				t.Fatal("paused domain was checked")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("domains not checked: %+v", st.Snapshot().Domains)
}

func TestVPS(t *testing.T) {
	m, st, _, n := setup(t)
	o := m.ovh.(*fakeOVH)
	var any, gra *store.VPS
	_ = st.Update(func(s *store.State) error {
		a, _ := s.AddVPS(store.VPS{Subsidiary: "FR", Plan: "vps-1", PlanName: "VPS-1"}, time.Now())
		g, _ := s.AddVPS(store.VPS{Subsidiary: "FR", Plan: "vps-1", PlanName: "VPS-1", Datacenters: []string{"eu-west-gra"}}, time.Now())
		any, gra = a, g
		return nil
	})
	ctx := context.Background()
	check := func(id string) []string {
		m.check(ctx, VPSKey(id))
		var titles []string
		for i, s := range n.take() {
			if i%2 == 0 {
				titles = append(titles, s.title+" | "+s.kind)
			}
		}
		return titles
	}
	steps := []struct {
		stock []string
		err   error
		any   string // titles for the watch of any datacenter
		gra   string // and for the one of Gravelines only
	}{
		{[]string{"eu-west-gra", "eu-west-sbg"}, nil, "", ""},
		{[]string{"eu-west-gra", "+eu-west-sbg"}, nil, "VPS-1 is available | success", ""},
		{nil, errors.New("timeout"), "", ""},
		{[]string{"+eu-west-gra", "+eu-west-sbg"}, nil, "VPS-1 is available | success", "VPS-1 is available | success"},
		{[]string{"+eu-west-gra", "eu-west-sbg"}, nil, "", ""},
		{[]string{"eu-west-gra", "eu-west-sbg"}, nil, "VPS-1 is out of stock | warning", "VPS-1 is out of stock | warning"},
	}
	for i, s := range steps {
		o.set("vps-1", s.err, s.stock...)
		if got := strings.Join(check(any.ID), ","); got != s.any {
			t.Errorf("step %d, any datacenter: sent %q, want %q", i, got, s.any)
		}
		if got := strings.Join(check(gra.ID), ","); got != s.gra {
			t.Errorf("step %d, Gravelines: sent %q, want %q", i, got, s.gra)
		}
	}

	v := st.Snapshot().VPSWatch(any.ID)
	if v.Status != store.OutOfStock || v.Error != "" || len(v.Stock) != 2 || len(v.History) != 5 {
		t.Fatalf("watch = %+v", v)
	}
	if v.History[2].Detail != "Gravelines, France; Strasbourg, France" {
		t.Errorf("history = %+v", v.History)
	}

	// The body names the new datacenters and links to the order page.
	o.set("vps-1", nil, "+eu-west-gra", "eu-west-sbg")
	m.check(ctx, VPSKey(gra.ID))
	if got := n.take(); len(got) != 2 || !strings.Contains(got[0].body, "in Gravelines, France: https://www.ovhcloud.com/") {
		t.Fatalf("sent %+v", got)
	}

	// A watch in stock is due after the stock interval, the others after
	// the regular one.
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	checked := now.Add(-3 * time.Minute)
	_ = st.Update(func(s *store.State) error {
		s.Settings.Interval, s.Settings.StockInterval = 60, 5
		for i := range s.VPS {
			s.VPS[i].Checked = &checked
		}
		return nil
	})
	if key, wait := m.next(); key != "" || wait != 2*time.Minute {
		t.Fatalf("next = %q, %v; want the watch in stock in 2 minutes", key, wait)
	}
	now = now.Add(2 * time.Minute)
	if key, _ := m.next(); key != VPSKey(gra.ID) {
		t.Fatalf("next = %q, want the watch in stock", key)
	}
	if v := st.Snapshot().VPSWatch(gra.ID); len(v.Periods) != 3 || v.Periods[0].End != nil || v.Periods[1].End == nil {
		t.Fatalf("periods = %+v", v.Periods)
	}

	// VPS watches are scheduled with the domains, and requested checks of
	// removed watches are skipped.
	_ = st.Update(func(s *store.State) error { return s.RemoveVPS(any.ID) })
	m.Check(VPSKey(any.ID), VPSKey(gra.ID))
	if key, _ := m.next(); key != VPSKey(gra.ID) {
		t.Fatalf("next = %q", key)
	}
}

func TestIntervals(t *testing.T) {
	m, st, _, _ := setup(t, "slow.com", "own.com")
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	at := func(ago time.Duration) *time.Time { t := now.Add(-ago); return &t }
	var vps, ownVPS, inStock string
	_ = st.Update(func(s *store.State) error {
		s.Settings.Interval, s.Settings.VPSInterval, s.Settings.StockInterval = 12*60, 15, 2
		s.Domain("slow.com").Checked = at(11 * time.Hour)
		s.Domain("own.com").Interval = 60
		s.Domain("own.com").Checked = at(50 * time.Minute)
		a, _ := s.AddVPS(store.VPS{Provider: store.OVH, Subsidiary: "FR", Plan: "vps-1", PlanName: "VPS-1"}, now)
		a.Checked = at(9 * time.Minute)
		b, _ := s.AddVPS(store.VPS{Provider: store.OVH, Subsidiary: "FR", Plan: "vps-2", PlanName: "VPS-2", Interval: 120}, now)
		b.Checked = at(115 * time.Minute)
		// An own interval longer than the in-stock one does not slow down
		// the checks while in stock.
		c, _ := s.AddVPS(store.VPS{Provider: store.OVH, Subsidiary: "FR", Plan: "vps-3", PlanName: "VPS-3", Interval: 120}, now)
		c.Status, c.Checked = store.Available, at(time.Minute)
		vps, ownVPS, inStock = a.ID, b.ID, c.ID
		return nil
	})

	// Due in: slow.com 60m, own.com 10m, VPS-1 6m, VPS-2 5m, VPS-3 1m.
	for _, want := range []struct {
		key  string
		wait time.Duration
	}{
		{VPSKey(inStock), time.Minute},
		{VPSKey(ownVPS), 5 * time.Minute},
		{VPSKey(vps), 6 * time.Minute},
		{"own.com", 10 * time.Minute},
		{"slow.com", time.Hour},
	} {
		if key, wait := m.next(); key != "" || wait != want.wait {
			t.Fatalf("next = %q, %v; want %s in %v", key, wait, want.key, want.wait)
		}
		now = now.Add(want.wait)
		key, _ := m.next()
		if key != want.key {
			t.Fatalf("next = %q, want %q", key, want.key)
		}
		// Pretend it was checked, and rewind the clock: the others are due
		// at the same times.
		_ = st.Update(func(s *store.State) error {
			never := now.Add(100 * 24 * time.Hour)
			if id, ok := strings.CutPrefix(key, "vps:"); ok {
				s.VPSWatch(id).Checked = &never
			} else {
				s.Domain(key).Checked = &never
			}
			return nil
		})
		now = now.Add(-want.wait)
	}
}

func TestMutedVPS(t *testing.T) {
	m, st, _, n := setup(t)
	o := m.ovh.(*fakeOVH)
	var id string
	_ = st.Update(func(s *store.State) error {
		v, _ := s.AddVPS(store.VPS{Provider: store.OVH, Subsidiary: "FR", Plan: "vps-1", PlanName: "VPS-1", Muted: true}, time.Now())
		id = v.ID
		return nil
	})
	ctx := context.Background()

	// A muted watch is still checked and records its stock, but notifies
	// no one.
	o.set("vps-1", nil, "eu-west-gra", "eu-west-sbg")
	m.check(ctx, VPSKey(id))
	o.set("vps-1", nil, "+eu-west-gra", "eu-west-sbg")
	m.check(ctx, VPSKey(id))
	v := st.Snapshot().VPSWatch(id)
	if got := n.take(); len(got) != 0 {
		t.Fatalf("muted watch sent %+v", got)
	}
	if v.Status != store.Available || len(v.Periods) != 1 || len(v.History) != 2 {
		t.Fatalf("muted watch not kept current: %+v", v)
	}

	// Unmuted, it notifies the next change, not the stock it already saw.
	_ = st.Update(func(s *store.State) error { s.VPSWatch(id).Muted = false; return nil })
	m.check(ctx, VPSKey(id))
	if got := n.take(); len(got) != 0 {
		t.Fatalf("unmuting notified the known stock: %+v", got)
	}
	o.set("vps-1", nil, "+eu-west-gra", "+eu-west-sbg")
	m.check(ctx, VPSKey(id))
	if got := n.take(); len(got) == 0 || got[0].title != "VPS-1 is available" {
		t.Fatalf("unmuted watch sent %+v", got)
	}
}
