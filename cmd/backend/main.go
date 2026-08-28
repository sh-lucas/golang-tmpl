package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rox-projects/golang-tmpl/internal/database"
	"github.com/rox-projects/golang-tmpl/internal/features/admins"
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

func run() error {
	databaseURI := env("DATABASE_URI", "data/app.db")
	jwtSecret := env("JWT_SECRET", "")
	if jwtSecret == "" {
		return errors.New("JWT_SECRET must be configured")
	}
	db, err := database.Open(context.Background(), databaseURI)
	if err != nil {
		return err
	}
	defer db.Close()

	mux := http.NewServeMux()
	admins.RegisterRoutes(mux, queries.New(db), jwtSecret)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
	})

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
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
