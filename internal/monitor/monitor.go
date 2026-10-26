// Package monitor checks the monitored domains and VPS plans periodically
// and sends notifications when their status changes.
//
// Checks run one at a time, with a pause in between, because registries
// rate-limit their RDAP and WHOIS servers per client, and OVH its API.
//
// Domains and VPS watches share one queue. They are identified by a key:
// the domain name, or "vps:" and the watch's ID (domain names cannot
// contain a colon).
package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/floriandejonckheere/dropcatch/internal/hetzner"
	"github.com/floriandejonckheere/dropcatch/internal/lookup"
	"github.com/floriandejonckheere/dropcatch/internal/notify"
	"github.com/floriandejonckheere/dropcatch/internal/ovh"
	"github.com/floriandejonckheere/dropcatch/internal/store"
	"github.com/floriandejonckheere/dropcatch/internal/webpush"
)

type Lookuper interface {
	Lookup(ctx context.Context, name string) (lookup.Result, error)
}

type StockChecker interface {
	Stock(ctx context.Context, subsidiary, plan string) ([]ovh.Datacenter, error)
}

type HetznerStock interface {
	Stock(ctx context.Context, plan string) ([]hetzner.Location, error)
}

type Notifier interface {
	Send(ctx context.Context, url, title, body, kind string) error
}

type Pusher interface {
	Send(ctx context.Context, sub webpush.Subscription, payload []byte, ttl time.Duration, urgency string) error
}

// Message is a notification about a domain or VPS.
type Message struct {
	Title, Body, Kind string
	// View is the part of the UI that push notifications open (domains,
	// vps or notifications); Tag replaces an earlier notification with
	// the same tag on the device.
	View, Tag string
}

type Monitor struct {
	store  *store.Store
	lookup Lookuper
	ovh    StockChecker
	notify Notifier
	// Push sends to push targets; nil when push is unavailable.
	Push Pusher
	// Hetzner checks Hetzner Cloud server types; nil when not configured.
	Hetzner HetznerStock

	// Pause between two checks.
	Pause time.Duration
	// Called after a domain, VPS watch or target changed, and when a check starts or
	// ends, so that clients can be updated.
	OnDomain   func(store.Domain)
	OnVPS      func(store.VPS)
	OnTargets  func()
	OnChecking func(keys []string)

	now  func() time.Time
	wake chan struct{}

	mu       sync.Mutex
	queue    []string // checks requested by the user, first in first out
	checking string
}

func New(s *store.Store, l Lookuper, o StockChecker, n Notifier) *Monitor {
	return &Monitor{
		store:      s,
		lookup:     l,
		ovh:        o,
		notify:     n,
		Pause:      2 * time.Second,
		OnDomain:   func(store.Domain) {},
		OnVPS:      func(store.VPS) {},
		OnTargets:  func() {},
		OnChecking: func([]string) {},
		now:        time.Now,
		wake:       make(chan struct{}, 1),
	}
}

// VPSKey returns the queue key of a VPS watch.
func VPSKey(id string) string { return vpsPrefix + id }

const vpsPrefix = "vps:"

// Run checks domains and VPS watches as they become due, until ctx is
// cancelled.
func (m *Monitor) Run(ctx context.Context) {
	for {
		name, wait := m.next()
		if name != "" {
			m.check(ctx, name)
			wait = m.Pause
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-m.wake:
			// Settings or domains changed: reconsider what is due, but
			// after a check, only once the pause is over.
			if name == "" {
				timer.Stop()
				continue
			}
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
		case <-timer.C:
		}
	}
}

// Wake makes Run reconsider which domain is due next, such as after a
// domain was added or the interval changed.
func (m *Monitor) Wake() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// Check queues the given keys (all active domains and VPS watches when
// none are given) to be checked as soon as possible.
func (m *Monitor) Check(names ...string) {
	if len(names) == 0 {
		st := m.store.Snapshot()
		for _, d := range st.Domains {
			if !d.Paused {
				names = append(names, d.Name)
			}
		}
		for _, v := range st.VPS {
			if !v.Paused {
				names = append(names, VPSKey(v.ID))
			}
		}
	}
	m.mu.Lock()
	for _, n := range names {
		if n != m.checking && !slices.Contains(m.queue, n) {
			m.queue = append(m.queue, n)
		}
	}
	m.mu.Unlock()
	m.publishChecking()
	m.Wake()
}

// Checking returns the key being checked and the queued ones.
func (m *Monitor) Checking() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []string{}
	if m.checking != "" {
		out = append(out, m.checking)
	}
	return append(out, m.queue...)
}

func (m *Monitor) publishChecking() { m.OnChecking(m.Checking()) }

// next returns the key to check now, or how long to wait for the next
// one to become due.
func (m *Monitor) next() (string, time.Duration) {
	st := m.store.Snapshot()
	m.mu.Lock()
	for len(m.queue) > 0 {
		key := m.queue[0]
		m.queue = m.queue[1:]
		if id, ok := strings.CutPrefix(key, vpsPrefix); (ok && st.VPSWatch(id) != nil) || (!ok && st.Domain(key) != nil) {
			m.mu.Unlock()
			return key, 0
		}
	}
	m.mu.Unlock()

	now := m.now()
	var due string
	var dueAt time.Time
	consider := func(key string, paused bool, checked *time.Time, interval time.Duration) {
		if paused {
			return
		}
		at := now
		if checked != nil {
			at = checked.Add(interval)
		}
		if due == "" || at.Before(dueAt) {
			due, dueAt = key, at
		}
	}
	for _, d := range st.Domains {
		consider(d.Name, d.Paused, d.Checked, st.Settings.DomainEvery(d))
	}
	for _, v := range st.VPS {
		// A VPS in stock is checked more often, to notice when it sells out.
		consider(VPSKey(v.ID), v.Paused, v.Checked, st.Settings.VPSEvery(v))
	}
	switch {
	case due == "":
		return "", time.Hour
	case !dueAt.After(now):
		return due, 0
	default:
		return "", dueAt.Sub(now)
	}
}

// check checks the domain or VPS watch with the given key.
func (m *Monitor) check(ctx context.Context, key string) {
	m.mu.Lock()
	m.checking = key
	m.mu.Unlock()
	m.publishChecking()
	defer func() {
		m.mu.Lock()
		m.checking = ""
		m.mu.Unlock()
		m.publishChecking()
	}()
	if id, ok := strings.CutPrefix(key, vpsPrefix); ok {
		m.checkVPS(ctx, id)
	} else {
		m.checkDomain(ctx, key)
	}
}

// checkDomain looks up one domain, records the result and sends
// notifications.
func (m *Monitor) checkDomain(ctx context.Context, name string) {
	st := m.store.Snapshot()
	lctx, cancel := context.WithTimeout(ctx, time.Duration(st.Settings.Timeout)*time.Second)
	res, err := m.lookup.Lookup(lctx, name)
	cancel()
	if ctx.Err() != nil {
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		err = errors.New("the registry did not answer in time")
	}

	now := m.now().UTC()
	var msg *Message
	var updated store.Domain
	uerr := m.store.Update(func(s *store.State) error {
		d := s.Domain(name)
		if d == nil {
			return store.ErrNotFound // removed while checking
		}
		d.Checked = &now
		if err != nil {
			d.Error = err.Error()
			updated = *d
			return nil
		}
		prev := d.Status
		apply(d, res)
		d.SetStatus(string(res.Status), now)
		msg = message(*d, prev, s.Settings.NotifyChanges)
		updated = *d
		return nil
	})
	if errors.Is(uerr, store.ErrNotFound) {
		return
	}
	if uerr != nil {
		slog.Error("could not save check result", "domain", name, "err", uerr)
		return
	}
	if err != nil {
		slog.Warn("check failed", "domain", name, "err", err)
	} else {
		slog.Info("checked", "domain", name, "status", updated.Status, "source", updated.Source)
	}
	m.OnDomain(updated)
	if msg != nil {
		m.Send(ctx, *msg)
	}
}

func apply(d *store.Domain, r lookup.Result) {
	d.Error = ""
	d.Statuses, d.Registrar, d.Source, d.Server = r.Statuses, r.Registrar, r.Source, r.Server
	d.Created, d.Expires = timePtr(r.Created), timePtr(r.Expires)
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// message returns the notification for a status change from prev, or nil.
func message(d store.Domain, prev string, changes bool) *Message {
	name := lookup.Display(d.Name)
	if name != d.Name {
		name += " (" + d.Name + ")"
	}
	switch {
	case d.Status == prev:
		return nil
	case d.Status == store.Available:
		return &Message{
			Title: name + " is available",
			Body:  name + " is not registered anymore and can be registered now.",
			Kind:  notify.Success,
			View:  "domains", Tag: d.Name,
		}
	case !changes || prev == store.Unknown:
		return nil
	case d.Status == store.Dropping:
		return &Message{
			Title: name + " is about to be deleted",
			Body:  fmt.Sprintf("The registration of %s expired (%s). The registry deletes it at the end of this period, unless the owner restores it.", name, statusList(d.Statuses)),
			Kind:  notify.Warning,
			View:  "domains", Tag: d.Name,
		}
	case prev == store.Available:
		return &Message{
			Title: name + " was registered",
			Body:  name + " was registered again" + by(d.Registrar) + ".",
			Kind:  notify.Failure,
			View:  "domains", Tag: d.Name,
		}
	case prev == store.Dropping:
		return &Message{
			Title: name + " was restored",
			Body:  "The owner of " + name + " restored or renewed it" + by(d.Registrar) + ", so it will not become available.",
			Kind:  notify.Info,
			View:  "domains", Tag: d.Name,
		}
	}
	return nil
}

func statusList(statuses []string) string {
	var out []string
	for _, s := range statuses {
		if strings.Contains(s, "delete") || strings.Contains(s, "redemption") || s == "quarantine" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return "deletion pending"
	}
	return strings.Join(out, ", ")
}

func by(registrar string) string {
	if registrar == "" {
		return ""
	}
	return " through " + registrar
}

// Send delivers msg to every enabled target, and records the outcome on
// each target.
func (m *Monitor) Send(ctx context.Context, msg Message) {
	for _, t := range m.store.Snapshot().Targets {
		if t.Enabled {
			_ = m.SendTo(ctx, t.ID, msg)
		}
	}
}

// SendTo delivers msg to one target.
func (m *Monitor) SendTo(ctx context.Context, id string, msg Message) error {
	st := m.store.Snapshot()
	t := st.Target(id)
	if t == nil {
		return store.ErrNotFound
	}
	sctx, cancel := context.WithTimeout(ctx, time.Minute)
	var err error
	if t.Type == store.Push {
		err = m.push(sctx, *t, msg)
	} else {
		err = m.notify.Send(sctx, t.URL, msg.Title, msg.Body, msg.Kind)
	}
	cancel()
	gone := errors.Is(err, webpush.ErrGone)
	if gone {
		err = errors.New("the device unsubscribed (notifications were turned off, or the app removed); add it again from that device")
	}
	if err != nil {
		slog.Warn("notification failed", "target", t.Name, "err", err)
	} else {
		slog.Info("notification sent", "target", t.Name, "title", msg.Title)
	}
	now := m.now().UTC()
	_ = m.store.Update(func(s *store.State) error {
		t := s.Target(id)
		if t == nil {
			return store.ErrNotFound
		}
		t.LastSent = &now
		t.LastError = ""
		if err != nil {
			t.LastError = err.Error()
		}
		if gone {
			// Sending again can never succeed.
			t.Enabled = false
		}
		return nil
	})
	m.OnTargets()
	return err
}

// push sends msg to a push target. Availability wakes the device at once;
// the push service keeps messages for a day while it is offline.
func (m *Monitor) push(ctx context.Context, t store.Target, msg Message) error {
	if m.Push == nil {
		return errors.New("push notifications are not available")
	}
	if t.Push == nil {
		return errors.New("the target has no push subscription")
	}
	payload, err := json.Marshal(map[string]string{"title": msg.Title, "body": msg.Body, "kind": msg.Kind, "view": msg.View, "tag": msg.Tag})
	if err != nil {
		return err
	}
	urgency := webpush.UrgencyNormal
	if msg.Kind == notify.Success {
		urgency = webpush.UrgencyHigh
	}
	return m.Push.Send(ctx, webpush.Subscription(*t.Push), payload, 24*time.Hour, urgency)
}
