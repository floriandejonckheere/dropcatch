package monitor

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/floriandejonckheere/dropcatch/internal/hetzner"
	"github.com/floriandejonckheere/dropcatch/internal/notify"
	"github.com/floriandejonckheere/dropcatch/internal/store"
)

// Labels on the servers an order creates: the watch, and the round of
// orders (Order.Batch).
const (
	watchLabel = "dropcatch"
	batchLabel = "dropcatch-batch"
)

// order creates the servers of the watch's order when its server type is
// in stock where the order wants it, one at a time, until the order is
// complete or Hetzner refuses one.
//
// Notifications about orders are sent also when the watch is muted: they
// are about servers that cost money, not about stock.
func (m *Monitor) order(ctx context.Context, v store.VPS) {
	o := v.Order
	// A paused watch can still be checked on demand, but does not order.
	if v.Provider != store.Hetzner || o == nil || !o.Active || o.Done() || v.Paused {
		return
	}
	loc, ok := orderLocation(v)
	if !ok {
		return
	}
	st := m.store.Snapshot()
	p := st.Project(o.Project)
	if p == nil {
		m.orderFailed(ctx, v, errors.New("its Hetzner project was removed"))
		return
	}
	timeout := time.Duration(st.Settings.Timeout) * time.Second

	// The project's servers: their names are taken, and those of this
	// round count, also when their creation was not confirmed.
	lctx, cancel := context.WithTimeout(ctx, timeout)
	existing, err := m.Hetzner.Servers(lctx, p.Token, "")
	cancel()
	if err != nil {
		m.orderFailed(ctx, v, err)
		return
	}
	taken := map[string]bool{}
	var found []store.Ordered
	for _, s := range existing {
		taken[s.Name] = true
		if s.Labels[watchLabel] == v.ID && s.Labels[batchLabel] == o.Batch {
			found = append(found, store.Ordered{ID: s.ID, Name: s.Name, Location: s.Location, Created: m.now().UTC()})
		}
	}
	for _, s := range o.Servers {
		taken[s.Name] = true
	}
	if v, ok = m.updateOrder(v, func(o *store.Order) { addServers(o, found) }); !ok || v.Order.Done() {
		return
	}

	var made []string
	var failure error
	for n := 1; !v.Order.Done(); n++ {
		name := fmt.Sprintf("%s-%d", v.Order.Name, n)
		if taken[name] {
			continue
		}
		taken[name] = true
		keys := make([]int64, len(v.Order.SSHKeys))
		for i, k := range v.Order.SSHKeys {
			keys[i] = k.ID
		}
		cctx, cancel := context.WithTimeout(ctx, timeout)
		s, err := m.Hetzner.Create(cctx, p.Token, hetzner.CreateServer{
			Name: name, ServerType: v.Plan, Location: loc.Code, Image: v.Order.Image.ID, SSHKeys: keys,
			Labels: map[string]string{watchLabel: v.ID, batchLabel: v.Order.Batch},
		})
		cancel()
		if err != nil {
			failure = err
			break
		}
		slog.Info("ordered server", "vps", v.Plan, "server", s.Name, "location", loc.Code, "project", p.Name)
		made = append(made, s.Name)
		now := m.now().UTC()
		if v, ok = m.updateOrder(v, func(o *store.Order) {
			addServers(o, []store.Ordered{{ID: s.ID, Name: s.Name, Location: cmp.Or(s.Location, loc.Code), Created: now}})
			o.Error, o.Tried = "", &now
			if o.Done() {
				o.Active = false
			}
		}); !ok {
			return
		}
	}

	if len(made) > 0 {
		o := v.Order
		title := fmt.Sprintf("Ordered %d × %s", len(made), v.PlanName)
		body := fmt.Sprintf("Created %s in %s, in the Hetzner Cloud project %s.", strings.Join(made, ", "), loc.Name, p.Name)
		if !o.Done() {
			title = fmt.Sprintf("Ordered %d of %d × %s", len(o.Servers), o.Quantity, v.PlanName)
			body += fmt.Sprintf(" %d more will be ordered when it is in stock again.", o.Quantity-len(o.Servers))
		}
		m.Send(ctx, Message{Title: title, Body: body, Kind: notify.Success, View: "vps", Tag: "order:" + v.ID})
	}
	if failure != nil {
		m.orderFailed(ctx, v, failure)
	}
}

// orderLocation returns where the order creates servers: its location
// when that has stock, or else the first watched location with stock.
func orderLocation(v store.VPS) (store.Stock, bool) {
	for _, s := range v.Stock {
		if s.Available && (v.Order.Location == s.Code || v.Order.Location == "" && v.Watches(s.Code)) {
			return s, true
		}
	}
	return store.Stock{}, false
}

// orderFailed records why servers could not be ordered. When trying again
// may work (the stock ran out first, or Hetzner was busy), the order is
// tried again at the next check that sees stock; otherwise it stops, with
// a notification.
func (m *Monitor) orderFailed(ctx context.Context, v store.VPS, err error) {
	retry := hetzner.Unavailable(err)
	now := m.now().UTC()
	v, ok := m.updateOrder(v, func(o *store.Order) {
		o.Error, o.Tried = err.Error(), &now
		if !retry {
			o.Active = false
		}
	})
	if !ok {
		return
	}
	slog.Warn("could not order server", "vps", v.Plan, "retry", retry, "err", err)
	if !retry {
		m.Send(ctx, Message{
			Title: "Could not order " + v.PlanName,
			Body:  fmt.Sprintf("Dropcatch stopped ordering %s: %v", v.PlanName, err),
			Kind:  notify.Failure, View: "vps", Tag: "order:" + v.ID,
		})
	}
}

// updateOrder changes the order of the watch, as long as it is the same
// round of orders (the user may have changed it meanwhile), and returns
// the updated watch.
func (m *Monitor) updateOrder(v store.VPS, fn func(*store.Order)) (store.VPS, bool) {
	batch := v.Order.Batch
	var updated store.VPS
	err := m.store.Update(func(s *store.State) error {
		w := s.VPSWatch(v.ID)
		if w == nil || w.Order == nil || w.Order.Batch != batch {
			return store.ErrNotFound
		}
		fn(w.Order)
		updated = *w
		return nil
	})
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			slog.Error("could not save order", "vps", v.Plan, "err", err)
		}
		return v, false
	}
	m.OnVPS(updated)
	return updated, true
}

// addServers adds the servers the order does not have yet.
func addServers(o *store.Order, servers []store.Ordered) {
	for _, s := range servers {
		known := false
		for _, k := range o.Servers {
			known = known || k.ID == s.ID
		}
		if !known {
			o.Servers = append(o.Servers, s)
		}
	}
	if o.Done() {
		o.Active = false
	}
}
