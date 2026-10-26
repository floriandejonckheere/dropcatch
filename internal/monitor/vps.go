package monitor

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/floriandejonckheere/dropcatch/internal/hetzner"
	"github.com/floriandejonckheere/dropcatch/internal/notify"
	"github.com/floriandejonckheere/dropcatch/internal/ovh"
	"github.com/floriandejonckheere/dropcatch/internal/store"
)

// checkVPS asks OVH for the stock of one VPS watch, records the result and
// sends notifications.
func (m *Monitor) checkVPS(ctx context.Context, id string) {
	st := m.store.Snapshot()
	v := st.VPSWatch(id)
	if v == nil {
		return
	}
	sctx, cancel := context.WithTimeout(ctx, time.Duration(st.Settings.Timeout)*time.Second)
	stock, err := m.stock(sctx, *v)
	cancel()
	if ctx.Err() != nil {
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		err = errors.New(store.ProviderName(v.Provider) + " did not answer in time")
	}

	now := m.now().UTC()
	var msg *Message
	var updated store.VPS
	uerr := m.store.Update(func(s *store.State) error {
		v := s.VPSWatch(id)
		if v == nil {
			return store.ErrNotFound // removed while checking
		}
		v.Checked = &now
		if err != nil {
			v.Error = err.Error()
			updated = *v
			return nil
		}
		v.Error = ""
		before := *v
		v.SetStock(stock, now)
		msg = vpsMessage(before, *v, s.Settings.NotifyChanges)
		updated = *v
		return nil
	})
	if errors.Is(uerr, store.ErrNotFound) {
		return
	}
	if uerr != nil {
		slog.Error("could not save check result", "vps", v.Plan, "err", uerr)
		return
	}
	if err != nil {
		slog.Warn("check failed", "vps", v.Plan, "subsidiary", v.Subsidiary, "err", err)
	} else {
		slog.Info("checked", "vps", v.Plan, "subsidiary", v.Subsidiary, "status", updated.Status)
	}
	m.OnVPS(updated)
	switch {
	case msg == nil:
	case updated.Muted:
		slog.Info("notification muted", "vps", updated.Plan, "title", msg.Title)
	default:
		m.Send(ctx, *msg)
	}
}

// stock asks the watch's provider for the stock of its plan in every
// datacenter or location that offers it.
func (m *Monitor) stock(ctx context.Context, v store.VPS) ([]store.Stock, error) {
	var stock []store.Stock
	switch v.Provider {
	case store.Hetzner:
		if m.Hetzner == nil {
			return nil, hetzner.ErrNoToken
		}
		locs, err := m.Hetzner.Stock(ctx, v.Plan)
		if err != nil {
			return nil, err
		}
		for _, l := range locs {
			stock = append(stock, store.Stock{Code: l.Code, Name: l.Name, Region: l.Region, Available: l.Available})
		}
	default:
		dcs, err := m.ovh.Stock(ctx, v.Subsidiary, v.Plan)
		if err != nil {
			return nil, err
		}
		for _, d := range dcs {
			stock = append(stock, store.Stock{Code: d.Code, Name: d.Name, Region: string(d.Region), Available: d.Available})
		}
	}
	return stock, nil
}

// orderURL returns where the watch's plan can be ordered.
func orderURL(v store.VPS) string {
	if v.Provider == store.Hetzner {
		return hetzner.OrderURL
	}
	return ovh.OrderURL(v.Subsidiary, v.Plan)
}

// vpsMessage returns the notification for the change from before to after,
// or nil. Stock appearing in a watched datacenter is always notified, also
// when another one already had stock.
func vpsMessage(before, after store.VPS, changes bool) *Message {
	had := map[string]bool{}
	if before.Status != store.Unknown {
		for _, s := range before.InStock() {
			had[s.Code] = true
		}
	}
	var added []string
	for _, s := range after.InStock() {
		if !had[s.Code] {
			added = append(added, s.Name)
		}
	}
	name := after.PlanName
	if len(added) > 0 {
		return &Message{
			Title: name + " is available",
			Body:  store.ProviderName(after.Provider) + " " + name + " (" + after.Plan + ") can be ordered in " + strings.Join(added, "; ") + ": " + orderURL(after),
			Kind:  notify.Success,
			View:  "vps", Tag: "vps:" + after.ID,
		}
	}
	if changes && before.Status == store.Available && after.Status == store.OutOfStock {
		return &Message{
			Title: name + " is out of stock",
			Body:  store.ProviderName(after.Provider) + " " + name + " (" + after.Plan + ") sold out again in the datacenters you watch.",
			Kind:  notify.Warning,
			View:  "vps", Tag: "vps:" + after.ID,
		}
	}
	return nil
}
