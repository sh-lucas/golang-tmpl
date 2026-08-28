package database_test

import (
	"context"
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
	for _, pragma := range []string{"cache_size", "mmap_size", "synchronous"} {
		var value string
		if err := db.QueryRow("PRAGMA " + pragma).Scan(&value); err != nil || value == "" {
			t.Fatalf("pragma %s: %v", pragma, err)
		}
	}
}
