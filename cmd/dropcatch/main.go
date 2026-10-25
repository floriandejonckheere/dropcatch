package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/floriandejonckheere/dropcatch/internal/api"
	"github.com/floriandejonckheere/dropcatch/internal/config"
	"github.com/floriandejonckheere/dropcatch/internal/hetzner"
	"github.com/floriandejonckheere/dropcatch/internal/lookup"
	"github.com/floriandejonckheere/dropcatch/internal/monitor"
	"github.com/floriandejonckheere/dropcatch/internal/notify"
	"github.com/floriandejonckheere/dropcatch/internal/ovh"
	"github.com/floriandejonckheere/dropcatch/internal/store"
	"github.com/floriandejonckheere/dropcatch/internal/webpush"
	"github.com/floriandejonckheere/dropcatch/web"
)

func main() {
	if err := run(); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			slog.Error("fatal", "err", err)
		}
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Parse(os.Args[1:], os.Getenv)
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return err
	}

	apprise := &notify.Apprise{Path: cfg.Apprise}
	if cfg.Apprise == "" {
		slog.Warn("apprise not found: notifications cannot be sent")
	}
	lc := lookup.New("dropcatch/" + api.Version)
	lc.BootstrapURL = cfg.RDAPBootstrap
	oc := ovh.New("dropcatch/" + api.Version)
	oc.BaseURL = cfg.OVHAPI
	hc := hetzner.New("dropcatch/"+api.Version, cfg.HetznerToken)
	hc.BaseURL = cfg.HetznerAPI
	mon := monitor.New(st, lc, oc, apprise)
	if hc.Configured() {
		mon.Hetzner = hc
	}
	push, err := webpush.New(filepath.Join(cfg.DataDir, "vapid.key"))
	if err != nil {
		return err
	}
	mon.Push = push

	server, err := api.New(cfg, st, mon, oc, hc, apprise, push.PublicKey(), web.Dist())
	if err != nil {
		return err
	}
	if cfg.AuthEnabled() {
		slog.Info("authentication enabled")
	} else {
		slog.Warn("authentication disabled: anyone who can reach Dropcatch can see and change your domains and notification URLs")
	}

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go mon.Run(ctx)

	errc := make(chan error, 1)
	go func() {
		slog.Info("dropcatch listening", "addr", cfg.Listen, "data", cfg.DataDir, "version", api.Version)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
