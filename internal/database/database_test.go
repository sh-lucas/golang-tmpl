package database_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/rox-projects/golang-tmpl/internal/database"
)

func TestOpenMigratesDatabase(t *testing.T) {
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var table string
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'admins'").Scan(&table); err != nil {
		t.Fatal(err)
	}
	assertPragma(t, db, "cache_size", -65536)
	assertPragmaAtLeast(t, db, "mmap_size", 128<<20)
	assertPragma(t, db, "synchronous", 1)
	if got := db.Stats().MaxOpenConnections; got != 15 {
		t.Fatalf("maximum open connections = %d, want 15", got)
	}
}

func assertPragma(t *testing.T, db interface{ QueryRow(string, ...any) *sql.Row }, name string, want int64) {
	t.Helper()
	var got int64
	if err := db.QueryRow("PRAGMA " + name).Scan(&got); err != nil {
		t.Fatalf("pragma %s: %v", name, err)
	}
	if got != want {
		t.Fatalf("pragma %s = %d, want %d", name, got, want)
	}
}

func assertPragmaAtLeast(t *testing.T, db interface{ QueryRow(string, ...any) *sql.Row }, name string, minimum int64) {
	t.Helper()
	var got int64
	if err := db.QueryRow("PRAGMA " + name).Scan(&got); err != nil {
		t.Fatalf("pragma %s: %v", name, err)
	}
	if got < minimum {
		t.Fatalf("pragma %s = %d, want at least %d", name, got, minimum)
	}
}
