// Command geotracker is the whole application: web UI, API, ingest and workers in one binary.
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
	"path/filepath"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // TZ works even in minimal container images

	"geotracker/internal/server"
	"geotracker/internal/store"
	"geotracker/web"
)

var version = "dev" // set at build time: -ldflags "-X main.version=1.2.3"

const usage = `GeoTracker %s — self-hosted location timeline

Usage:
  geotracker [serve]                     run the server (default)
  geotracker backup [file.db.gz]         write a backup now
  geotracker restore <file>              stage a backup; applied on next start
  geotracker reset-password <email>      set a new password (reads it from GT_NEW_PASSWORD)
  geotracker version

Environment:
  GT_DATA_DIR          data directory (default ./data, /data in Docker)
  GT_LISTEN            listen address (default :8080)
  GT_BASE_URL          public URL, e.g. https://track.example.com
  GT_LOG_LEVEL         debug | info | warn | error (default info)
  GT_LOG_FORMAT        text | json (default text)
  GT_TRUSTED_PROXIES   comma-separated CIDRs allowed to set X-Forwarded-For
  GT_ADMIN_EMAIL, GT_ADMIN_PASSWORD   create the first admin without the setup wizard
`

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	setupLogging()
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	dataDir := env("GT_DATA_DIR", "data")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch cmd {
	case "serve":
		err = serve(ctx, dataDir)
	case "backup":
		err = backup(ctx, dataDir)
	case "restore":
		if len(os.Args) < 3 {
			err = errors.New("usage: geotracker restore <file>")
			break
		}
		if err = store.StageRestore(ctx, dataDir, os.Args[2]); err == nil {
			fmt.Println("Backup verified and staged. It replaces the current database on the next start;")
			fmt.Println("a safety copy of the current database is written to backups/ first.")
		}
	case "reset-password":
		err = resetPassword(ctx, dataDir)
	case "healthcheck": // for Docker HEALTHCHECK: the distroless image has no curl
		addr := env("GT_LISTEN", ":8080")
		if strings.HasPrefix(addr, ":") {
			addr = "127.0.0.1" + addr
		}
		resp, herr := (&http.Client{Timeout: 3 * time.Second}).Get("http://" + addr + "/healthz")
		if herr != nil {
			os.Exit(1)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
	case "version":
		fmt.Println(version)
	default:
		fmt.Printf(usage, version)
		if cmd != "help" && cmd != "-h" && cmd != "--help" {
			os.Exit(2)
		}
	}
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

func setupLogging() {
	var level slog.Level
	level.UnmarshalText([]byte(env("GT_LOG_LEVEL", "info")))
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler = slog.NewTextHandler(os.Stderr, opts)
	if env("GT_LOG_FORMAT", "text") == "json" {
		h = slog.NewJSONHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(h))
}

func serve(ctx context.Context, dataDir string) error {
	db, err := store.Open(ctx, dataDir)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := bootstrapAdmin(ctx, db); err != nil {
		return err
	}

	var proxies []netip.Prefix
	for _, c := range strings.Split(os.Getenv("GT_TRUSTED_PROXIES"), ",") {
		if c = strings.TrimSpace(c); c != "" {
			p, err := netip.ParsePrefix(c)
			if err != nil {
				return fmt.Errorf("GT_TRUSTED_PROXIES: %w", err)
			}
			proxies = append(proxies, p)
		}
	}
	baseURL := strings.TrimRight(os.Getenv("GT_BASE_URL"), "/")
	srv := server.New(server.Config{DataDir: dataDir, BaseURL: baseURL, Version: version, TrustedProxies: proxies, Web: web.Dist()}, db)
	defer srv.Close()

	workCtx, cancelWork := context.WithCancel(ctx)
	defer cancelWork()
	if err := srv.Run(workCtx); err != nil {
		return err
	}

	addr := env("GT_LISTEN", ":8080")
	hs := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		// No global write timeout: exports and the live stream are long-lived.
	}
	errc := make(chan error, 1)
	go func() { errc <- hs.ListenAndServe() }()
	abs, _ := filepath.Abs(dataDir)
	slog.Info("GeoTracker started", "version", version, "listen", addr, "data", abs, "base_url", baseURL)
	if baseURL == "" {
		slog.Warn("GT_BASE_URL is not set; phone setup links will use the address you browse from")
	}

	restart := false
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	case <-srv.Restart:
		restart = true
	}
	slog.Info("shutting down")
	cancelWork()
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hs.Shutdown(shutCtx)
	if restart {
		// Docker (restart: unless-stopped) and systemd (Restart=always) start us again,
		// and store.Open applies the staged restore.
		slog.Warn("exiting to apply the restored backup; the service manager restarts GeoTracker")
	}
	return nil
}

func bootstrapAdmin(ctx context.Context, db *store.Store) error {
	email, pw := os.Getenv("GT_ADMIN_EMAIL"), os.Getenv("GT_ADMIN_PASSWORD")
	if email == "" || pw == "" {
		return nil
	}
	if len(pw) < 10 {
		return errors.New("GT_ADMIN_PASSWORD must be at least 10 characters")
	}
	_, err := db.CreateFirstAdmin(ctx, strings.ToLower(email), "Admin", server.HashPassword(pw))
	if errors.Is(err, store.ErrExists) {
		return nil
	}
	if err == nil {
		slog.Info("created admin from environment", "email", email)
	}
	return err
}

func backup(ctx context.Context, dataDir string) error {
	db, err := store.Open(ctx, dataDir)
	if err != nil {
		return err
	}
	defer db.Close()
	dest := filepath.Join(dataDir, "backups", "manual-"+time.Now().Format("20060102-150405")+".db.gz")
	if len(os.Args) > 2 {
		dest = os.Args[2]
	}
	if err := db.Backup(ctx, dest); err != nil {
		return err
	}
	fmt.Println("Backup written to", dest)
	return nil
}

func resetPassword(ctx context.Context, dataDir string) error {
	if len(os.Args) < 3 {
		return errors.New("usage: GT_NEW_PASSWORD=... geotracker reset-password <email>")
	}
	pw := os.Getenv("GT_NEW_PASSWORD")
	if len(pw) < 10 {
		return errors.New("set GT_NEW_PASSWORD to the new password (at least 10 characters)")
	}
	db, err := store.Open(ctx, dataDir)
	if err != nil {
		return err
	}
	defer db.Close()
	u, err := db.UserByEmail(ctx, strings.ToLower(os.Args[2]))
	if err != nil {
		return fmt.Errorf("user %s: %w", os.Args[2], err)
	}
	if err := db.SetPassword(ctx, u.ID, server.HashPassword(pw), nil); err != nil {
		return err
	}
	fmt.Println("Password updated; all sessions of", u.Email, "were signed out.")
	return nil
}
