package database_test

import (
	"context"
	"database/sql"
	"os"
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

func TestCheckpointTruncatesWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := database.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE checkpoint_test (value TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO checkpoint_test (value) VALUES ('written to wal')"); err != nil {
		t.Fatal(err)
	}
	if err := database.Checkpoint(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(path + suffix); !os.IsNotExist(err) {
			t.Fatalf("database sidecar %s remains after close: %v", suffix, err)
		}
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
