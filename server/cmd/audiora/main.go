// Command audiora is the Audiora streaming server: a single binary serving
// the JSON API and the audio itself, backed by SQLite.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/audiora/audiora/server/internal/api"
	"github.com/audiora/audiora/server/internal/auth"
	"github.com/audiora/audiora/server/internal/config"
	"github.com/audiora/audiora/server/internal/db"
	"github.com/audiora/audiora/server/internal/media"
	"github.com/audiora/audiora/server/internal/scan"
	"github.com/audiora/audiora/server/internal/scrobble"
	"github.com/audiora/audiora/server/internal/sync"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	if err := cfg.EnsureDirs(); err != nil {
		return fmt.Errorf("data directories: %w", err)
	}

	database, err := db.Open(cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer database.Close()

	if _, err := os.Stat(cfg.MusicPath); err != nil {
		return fmt.Errorf("music path %s is not readable: %w", cfg.MusicPath, err)
	}

	tokens := auth.NewTokenIssuer(cfg.Secret, cfg.AccessTokenTTL)
	authStore := auth.NewStore(database, tokens, cfg.RefreshTokenTTL)
	authenticator := &auth.Authenticator{Issuer: tokens, Store: authStore}

	transcoder, err := media.NewTranscoder(cfg.MusicPath, cfg.TranscodeCacheDir)
	if err != nil {
		return fmt.Errorf("audio transcoder: %w", err)
	}

	// Background workers all share this context, so a shutdown stops them
	// together. It is created before the scanner because the scanner is given
	// this lifetime rather than the lifetime of the request that triggers it:
	// an admin pressing "Scan" would otherwise cancel the scan immediately.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scanner := scan.New(database, scan.Options{
		MusicRoot:     cfg.MusicPath,
		Base:          ctx,
		ExtractCovers: cfg.ExtractCoverArt,
		ExtractColors: cfg.ExtractColors,
		CoverDir:      cfg.CoverArtDir,
	})

	hub := sync.NewHub(database)
	scrobbler := scrobble.NewService(database)

	server := api.New(api.Deps{
		Config:     cfg,
		DB:         database,
		Auth:       authStore,
		Tokens:     tokens,
		AuthMW:     authenticator,
		Transcoder: transcoder,
		Scanner:    scanner,
		Hub:        hub,
		Scrobbler:  scrobbler,
	})

	if err := bootstrapAdmin(authStore, cfg); err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}

	scrobbler.Start()
	defer scrobbler.Stop()

	go runScanLoop(ctx, scanner, cfg)
	go runMaintenanceLoop(ctx, authStore, transcoder, scrobbler)

	srv := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: server,
		// No WriteTimeout: audio responses are long-lived streams, and a
		// write deadline would sever playback partway through a track.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("audiora listening",
			"addr", cfg.ListenAddr, "music", cfg.MusicPath, "data", cfg.DataPath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case sig := <-stop:
		slog.Info("shutting down", "signal", sig.String())
	}

	// Give in-flight audio a moment to finish rather than cutting people off
	// mid-song on a container restart.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	return srv.Shutdown(shutdownCtx)
}

// bootstrapAdmin creates the first account from the environment on a fresh
// install. It is a no-op once any user exists, so changing the env vars later
// does not resurrect a deleted account.
func bootstrapAdmin(store *auth.Store, cfg *config.Config) error {
	count, err := store.UserCount()
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if cfg.BootstrapAdminEmail == "" || cfg.BootstrapAdminPassword == "" {
		slog.Warn("no users exist and ADMIN_EMAIL/ADMIN_PASSWORD are not set; " +
			"the first visitor can create the admin account from the sign-in screen")
		return nil
	}

	user, err := store.CreateUser(cfg.BootstrapAdminEmail, cfg.BootstrapAdminPassword, cfg.BootstrapAdminName, true)
	if err != nil {
		return err
	}
	slog.Info("created the first administrator account", "email", user.Email)
	return nil
}

// runScanLoop re-scans the library on an interval so files added over SMB or
// by a sync client appear without anyone pressing a button.
func runScanLoop(ctx context.Context, scanner *scan.Scanner, cfg *config.Config) {
	if cfg.ScanInterval <= 0 {
		slog.Info("periodic scanning is disabled; use the admin UI to scan")
		return
	}
	ticker := time.NewTicker(cfg.ScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if scanner.IsRunning() {
				continue
			}
			slog.Info("starting scheduled library scan")
			scanner.Start(ctx)
			scanner.Wait()
		}
	}
}

// runMaintenanceLoop prunes dead sessions and stale transcoded files.
func runMaintenanceLoop(ctx context.Context, store *auth.Store, transcoder *media.Transcoder, _ *scrobble.Service) {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := store.PurgeExpiredSessions(); err == nil && n > 0 {
				slog.Info("pruned expired sessions", "count", n)
			}
			if n, err := transcoder.PruneCache(30 * 24 * time.Hour); err == nil && n > 0 {
				slog.Info("pruned stale transcodes", "count", n)
			}
		}
	}
}
