// Command stormkeep serves a live dashboard for a WeatherFlow
// Tempest station: current conditions from the station's own UDP broadcasts,
// and history from the database weewx keeps.
//
// Configuration is by environment variable; see docs/reference/configuration.md.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	_ "time/tzdata" // TZ works even in an image with no zoneinfo

	"github.com/runlevel-six/stormkeep/internal/archive"
	"github.com/runlevel-six/stormkeep/internal/live"
	"github.com/runlevel-six/stormkeep/internal/tempest"
	"github.com/runlevel-six/stormkeep/internal/web"
)

// version is stamped at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println(version)
		return
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("exiting", "err", err)
		os.Exit(1)
	}
}

type config struct {
	listen, db, udp, serial, name, units string
	altitude                             float64
	allowFrom                            []netip.Prefix
	allowIndexing                        bool
}

func loadConfig() (config, error) {
	c := config{
		listen: env("WX_LISTEN", ":8080"),
		db:     env("WX_DB", "/data/weewx.sdb"),
		udp:    env("WX_UDP", ":50222"),
		serial: os.Getenv("WX_STATION_SERIAL"),
		name:   env("WX_NAME", "Weather"),
		units:  env("WX_UNITS", "us"),
	}
	var errs []error
	if c.units != "us" && c.units != "metric" {
		errs = append(errs, fmt.Errorf("WX_UNITS must be us or metric, not %q", c.units))
	}
	if v := os.Getenv("WX_ALTITUDE"); v != "" {
		a, err := strconv.ParseFloat(v, 64)
		if err != nil {
			errs = append(errs, fmt.Errorf("WX_ALTITUDE: %w", err))
		}
		c.altitude = a
	}
	for _, s := range strings.Split(os.Getenv("WX_ALLOW_FROM"), ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			errs = append(errs, fmt.Errorf("WX_ALLOW_FROM: %w", err))
			continue
		}
		c.allowFrom = append(c.allowFrom, p.Masked())
	}
	if v := os.Getenv("WX_ALLOW_INDEXING"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("WX_ALLOW_INDEXING: %w", err))
		}
		c.allowIndexing = b
	}
	return c, errors.Join(errs...)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func run(log *slog.Logger) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	arc, err := archive.Open(cfg.db, cfg.altitude)
	if err != nil {
		return err
	}
	defer arc.Close()
	if err := arc.Ping(ctx); err != nil {
		// Not fatal: weewx creates the database on its first start, and the
		// live half of the page works without it.
		log.Warn("weewx database not readable yet", "path", cfg.db, "err", err)
	}

	station := live.New(cfg.serial)
	if cfg.udp != "" {
		go listen(ctx, log, cfg.udp, station)
	}

	srv := &http.Server{
		Addr: cfg.listen,
		Handler: web.Handler(web.Config{
			Name: cfg.name, Units: cfg.units, Altitude: cfg.altitude, Version: version,
			AllowFrom: cfg.allowFrom, AllowIndexing: cfg.allowIndexing,
			Archive: arc, Station: station, Log: log, Done: ctx.Done(),
		}),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		// No WriteTimeout: /api/stream is a response that never ends.
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("serving", "version", version, "addr", cfg.listen, "udp", cfg.udp, "db", cfg.db,
		"allow_from", len(cfg.allowFrom), "tz", time.Local.String())

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}

// listen runs the UDP listener, restarting it if it fails. A station that
// goes quiet is logged once, not once per missed packet.
func listen(ctx context.Context, log *slog.Logger, addr string, station *live.Station) {
	var lastBad atomic.Int64
	bad := func(err error) {
		now := time.Now().Unix()
		if prev := lastBad.Load(); now-prev >= 60 && lastBad.CompareAndSwap(prev, now) {
			log.Warn("undecodable datagram (logged at most once a minute)", "err", err)
		}
	}
	go watchSilence(ctx, log, station)
	for {
		err := tempest.Listen(ctx, addr, station.Handle, bad)
		if ctx.Err() != nil {
			return
		}
		log.Error("UDP listener stopped; restarting in 10s", "addr", addr, "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}

func watchSilence(ctx context.Context, log *slog.Logger, station *live.Station) {
	const quiet = 3 * time.Minute
	silent := false
	start := time.Now()
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		last := station.Snapshot().LastPacket
		if last.IsZero() {
			last = start
		}
		switch gone := time.Since(last) > quiet; {
		case gone && !silent:
			log.Warn("no broadcasts from the station", "since", last.Format(time.RFC3339))
		case !gone && silent:
			log.Info("station broadcasts resumed")
		}
		silent = time.Since(last) > quiet
	}
}
