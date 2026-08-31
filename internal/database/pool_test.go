package database

import (
	"database/sql"
	"testing"
)

func TestConfigurePoolOpensConnectionsOnDemand(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Ugly? Maybe. But it tests the sqlc behaviour.
	// The pool should open connections on demand, and allow for up to 15 concurrent.
	db.SetMaxOpenConns(15)
	db.SetMaxIdleConns(1)

	stats := db.Stats()
	if stats.MaxOpenConnections != 15 {
		t.Fatalf("maximum open connections = %d, want 15", stats.MaxOpenConnections)
	}
	if stats.OpenConnections != 0 {
		t.Fatalf("open connections before query = %d, want 0", stats.OpenConnections)
	}

	var value int
	if err := db.QueryRow("SELECT 1").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != 1 {
		t.Fatalf("query value = %d, want 1", value)
	}
	stats = db.Stats()
	if stats.OpenConnections != 1 {
		t.Fatalf("open connections after query = %d, want 1", stats.OpenConnections)
	}
	if stats.Idle != 1 {
		t.Fatalf("idle connections after query = %d, want 1", stats.Idle)
	}
}
