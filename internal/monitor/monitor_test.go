package monitor

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/floriandejonckheere/dropcatch/internal/lookup"
	"github.com/floriandejonckheere/dropcatch/internal/store"
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

type sent struct{ url, title, kind string }

type fakeNotifier struct {
	mu   sync.Mutex
	sent []sent
}

func (f *fakeNotifier) Send(_ context.Context, url, title, body, kind string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sent{url, title, kind})
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
	m := New(st, l, n)
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
	go m.Run(ctx)
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
