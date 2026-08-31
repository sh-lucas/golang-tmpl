package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rox-projects/golang-tmpl/migrations"
	_ "modernc.org/sqlite"
)

func Open(ctx context.Context, uri string) (*sql.DB, error) {
	if !strings.Contains(uri, ":memory:") && !strings.HasPrefix(uri, "file:") {
		if err := os.MkdirAll(filepath.Dir(uri), 0o750); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	separator := "?"
	if strings.Contains(uri, "?") {
		separator = "&"
	}
	dsn := uri + separator + strings.Join([]string{
		"_pragma=foreign_keys(1)", "_pragma=busy_timeout(5000)", "_pragma=journal_mode(WAL)",
		"_pragma=cache_size(-65536)", "_pragma=mmap_size(268435456)", "_pragma=synchronous(NORMAL)",
	}, "&")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// SQLite does not need a lot of connections,
	// and opens new ones on demand without TCP handshaking.
	db.SetMaxOpenConns(15)
	db.SetMaxIdleConns(1)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	if err := migrations.Run(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// Checkpoint writes all WAL pages into the main database file and truncates the
// WAL file. It must run after application-level database users have stopped.
func Checkpoint(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return fmt.Errorf("checkpoint database WAL: %w", err)
	}
	return nil
}
