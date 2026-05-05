package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zbloss/lantern/internal/config"
	"github.com/zbloss/lantern/internal/metadata"
	"github.com/zbloss/lantern/internal/metadata/sqlite"
	"github.com/zbloss/lantern/services/query/internal/auth"
)

// Run starts the Query API: opens the metadata store, bootstraps the admin user,
// and serves auth, span, analytics, and metrics endpoints.
func Run() error {
	cfg, err := config.Load("")
	if err != nil {
		return fmt.Errorf("query: load config: %w", err)
	}

	// Open metadata store
	store, err := openMetadataStore(cfg)
	if err != nil {
		return fmt.Errorf("query: open metadata store: %w", err)
	}
	defer store.Close()

	// Parse session TTL
	sessionTTL, err := time.ParseDuration(cfg.Auth.SessionTTL)
	if err != nil {
		sessionTTL = 168 * time.Hour // default 7 days
		slog.Warn("invalid session_ttl, using default 168h", "given", cfg.Auth.SessionTTL)
	}

	// Bootstrap admin user if no users exist
	h := auth.NewHandler(store, cfg.Auth.SecureCookie, sessionTTL, cfg.Auth.AdminEmail, cfg.Auth.AdminPassword)
	created, err := h.BootstrapAdmin(context.Background())
	if err != nil {
		return fmt.Errorf("query: bootstrap admin: %w", err)
	}
	if created {
		slog.Info("query: admin user bootstrapped", "email", cfg.Auth.AdminEmail)
	} else {
		if count, _ := store.CountUsers(context.Background()); count == 0 {
			slog.Warn("query: no admin configured and no users exist — set LANTERN_AUTH_ADMIN_EMAIL and LANTERN_AUTH_ADMIN_PASSWORD to create the first admin user")
		}
	}

	// Parse query listen address
	addr := cfg.Query.Addr
	if addr == "" {
		addr = ":8002"
	}

	// Create HTTP server
	mux := http.NewServeMux()

	// Register auth routes
	h.Register(mux)

	// TODO: Register other handlers (spans, analytics, prompts, scores)
	// mux.HandleFunc("POST /api/v1/spans/query", spanHandler.Query)
	// mux.HandleFunc("GET /api/v1/traces/{traceID}", spanHandler.GetTrace)
	// mux.HandleFunc("POST /api/v1/analytics/spans", analyticsHandler.Query)
	// mux.HandleFunc("GET /api/v1/prompts/{name}", promptHandler.Get)
	// mux.HandleFunc("POST /api/v1/scores", scoreHandler.Write)

	// Serve embedded UI (when built)
	// mux.Handle("/", http.FS(ui.FS))

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("query: listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("query: server error", "error", err)
		}
	}()

	<-ctx.Done()
	slog.Info("query: shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// openMetadataStore creates a metadata store from config.
func openMetadataStore(cfg *config.Config) (metadata.Store, error) {
	driver := cfg.Database.Driver
	dsn := cfg.Database.DSN

	switch driver {
	case "", "sqlite":
		if dsn == "" {
			dsn = "lantern.db"
		}
		slog.Info("query: opening SQLite metadata store", "path", dsn)
		store, err := sqlite.New(dsn)
		if err != nil {
			return nil, err
		}
		if err := store.Migrate(context.Background()); err != nil {
			store.Close()
			return nil, fmt.Errorf("query: migrate: %w", err)
		}
		return store, nil
	case "postgres":
		// TODO: implement postgres store
		return nil, fmt.Errorf("query: postgres metadata store not yet implemented")
	default:
		return nil, fmt.Errorf("query: unknown database driver: %s", driver)
	}
}
