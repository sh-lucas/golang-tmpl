package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rox-projects/golang-tmpl/docs"
	"github.com/rox-projects/golang-tmpl/internal/database"
	"github.com/rox-projects/golang-tmpl/internal/features/admins"
	"github.com/rox-projects/golang-tmpl/internal/features/health"
	"github.com/rox-projects/golang-tmpl/internal/features/libsql"
	"github.com/rox-projects/golang-tmpl/queries"
)

// @title Go HTTP template API
// @version 1.0
// @description Minimal Go 1.27 API using net/http, JSON v2, SQLite and sqlc.
// @BasePath /
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
func main() {
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
	admins.RegisterRoutes(mux, queries.New(db), jwtSecret)
	libsqlHandler := libsql.RegisterRoutes(mux, db, accessKey)
	defer func() {
		libsqlHandler.Close()
		checkpointCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := database.Checkpoint(checkpointCtx, db); err != nil && runErr == nil {
			runErr = err
		}
		if err := db.Close(); err != nil && runErr == nil {
			runErr = err
		}
	}()
	health.RegisterRoutes(mux, health.Options{DatabasePath: filepath.Join(databaseRoot, "sqlite.db")})
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("graceful shutdown", "error", err)
		}
	}()

	slog.Info("listening", "address", server.Addr)
	runErr = server.ListenAndServe()
	if errors.Is(runErr, http.ErrServerClosed) {
		return nil
	}
	return runErr
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
