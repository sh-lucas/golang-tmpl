package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rox-projects/golang-tmpl/docs"
	"github.com/rox-projects/golang-tmpl/internal/database"
	"github.com/rox-projects/golang-tmpl/internal/features/admins"
	"github.com/rox-projects/golang-tmpl/internal/features/health"
	"github.com/rox-projects/golang-tmpl/internal/features/libsql"
	// mkhandler:imports
)

// Requests get this long to finish after SIGINT/SIGTERM; closing the database
// afterwards is fast, so shutdown stays below the 10s `docker stop` grace period.
const drainTimeout = 5 * time.Second

// @title Go HTTP template API
// @version 1.0
// @description Minimal Go 1.27 API using net/http, JSON v2, SQLite and sqlc.
// @BasePath /
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
func main() {
	// One JSON object per line on stderr: readable by log collectors (Loki,
	// Docker drivers). log.Printf also goes through this handler.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() (runErr error) {
	databaseRoot := env("DATABASE_ROOT", "data")
	jwtSecret := env("JWT_SECRET", "")
	if jwtSecret == "" {
		return errors.New("JWT_SECRET must be configured")
	}
	accessKey := env("DATABASE_ACCESS_KEY", "")
	if accessKey == "" {
		return errors.New("DATABASE_ACCESS_KEY must be configured")
	}
	db, err := database.Open(context.Background(), filepath.Join(databaseRoot, "sqlite.db"))
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	admins.RegisterRoutes(mux, db, jwtSecret)
	libsqlHandler := libsql.RegisterRoutes(mux, db, accessKey, databaseRoot)
	// mkhandler:routes
	defer func() {
		libsqlHandler.Close()
		// Closing the last connection checkpoints the WAL and removes -wal/-shm.
		// No explicit checkpoint: under Litestream it must own the WAL.
		if err := db.Close(); err != nil && runErr == nil {
			runErr = err
		}
	}()
	health.RegisterRoutes(mux, health.Options{DatabasePath: filepath.Join(databaseRoot, "sqlite.db")})
	docsRoot := env("BOOK_ROOT", "book")
	mux.Handle("GET /docs/", http.StripPrefix("/docs/", http.FileServer(http.Dir(docsRoot))))
	mux.HandleFunc("GET /docs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs/", http.StatusPermanentRedirect)
	})
	mux.HandleFunc("GET /swagger", redirectSwagger)
	mux.HandleFunc("GET /swagger.json", serveSwagger)

	server := &http.Server{
		Addr:              ":" + strings.TrimPrefix(env("SERVER_PORT", "3000"), ":"),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	// Connections that have not sent a byte yet (browser preconnects, unused
	// client dials). net/http waits 5s for them on Shutdown, so we close them.
	var unused sync.Map
	server.ConnState = func(conn net.Conn, state http.ConnState) {
		if state == http.StateNew {
			unused.Store(conn, nil)
		} else {
			unused.Delete(conn)
		}
	}

	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	slog.Info("listening", "address", listener.Addr().String())

	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}
	// Restore default signal handling: a second Ctrl+C/SIGTERM kills immediately.
	stop()
	slog.Info("shutting down", "drain_timeout", drainTimeout.String())
	drainCtx, cancel := context.WithTimeout(context.Background(), drainTimeout)
	defer cancel()
	drained := make(chan error, 1)
	go func() { drained <- server.Shutdown(drainCtx) }()
	// Serve returns once the listener is closed: no connection is accepted after this.
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	unused.Range(func(conn, _ any) bool {
		_ = conn.(net.Conn).Close()
		return true
	})
	if err := <-drained; err != nil {
		slog.Warn("requests still running after drain timeout; closing them", "error", err)
		_ = server.Close()
	}
	// The deferred cleanup closes libSQL sessions and the database.
	return nil
}

func redirectSwagger(w http.ResponseWriter, request *http.Request) {
	http.Redirect(w, request, "/swagger.json", http.StatusTemporaryRedirect)
}

func serveSwagger(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(docs.SwaggerJSON)
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
