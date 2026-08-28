package database_test

import (
	"context"
	"testing"

	"github.com/rox-projects/golang-tmpl/internal/database"
)

func TestOpenMigratesDatabase(t *testing.T) {
	db, err := database.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var table string
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'admins'").Scan(&table); err != nil {
		t.Fatal(err)
	}
}
