package tests

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/rox-projects/golang-tmpl/database"
	"github.com/rox-projects/golang-tmpl/database/migrations"
)

var (
	migrationName = regexp.MustCompile(`^(\d{3})_[a-z0-9_]+\.sql$`)
	lineComment   = regexp.MustCompile(`--[^\n]*`)
	// Each migration already runs inside a transaction (migrations.Run):
	// transaction control breaks that, and PRAGMAs are ignored or fail there.
	forbiddenStatement = regexp.MustCompile(`(?im)^\s*(BEGIN(\s+(DEFERRED|IMMEDIATE|EXCLUSIVE))?(\s+TRANSACTION)?\s*;|COMMIT\b|END\s+TRANSACTION\b|ROLLBACK\s*(TRANSACTION\s*)?;|SAVEPOINT\b|RELEASE\b|PRAGMA\b|VACUUM\b)`)
)

func migrationFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir("../database/migrations")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		t.Fatal("no migrations found")
	}
	return names
}

func TestMigrationFilesAreWellFormed(t *testing.T) {
	for index, name := range migrationFiles(t) {
		match := migrationName.FindStringSubmatch(name)
		if match == nil {
			t.Errorf("%s: name must look like 001_snake_case.sql", name)
			continue
		}
		// Sequential numbers make ordering obvious and catch duplicates from parallel branches.
		if number, _ := strconv.Atoi(match[1]); number != index+1 {
			t.Errorf("%s: expected number %03d; migrations must be numbered without gaps or duplicates", name, index+1)
		}
		content, err := os.ReadFile(filepath.Join("../database/migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if statement := forbiddenStatement.FindString(lineComment.ReplaceAllString(string(content), "")); statement != "" {
			t.Errorf("%s: %q is not allowed; each migration already runs in its own transaction", name, strings.TrimSpace(statement))
		}
	}
}

func schema(t *testing.T, db *sql.DB) string {
	t.Helper()
	rows, err := db.Query("SELECT type || ' ' || name || ': ' || coalesce(sql, '') FROM sqlite_schema ORDER BY type, name")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}

func TestMigrationsApplyCleanlyAndOnlyOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sqlite.db")
	db, err := database.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var applied int
	if err := db.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if want := len(migrationFiles(t)); applied != want {
		t.Fatalf("applied migrations = %d, want %d", applied, want)
	}
	var integrity string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity check = %q, %v", integrity, err)
	}
	var violations int
	if err := db.QueryRow("SELECT count(*) FROM pragma_foreign_key_check").Scan(&violations); err != nil || violations != 0 {
		t.Fatalf("foreign key violations after migrations = %d, %v", violations, err)
	}

	// Restarting the app runs migrations again; nothing may change.
	before := schema(t, db)
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if after := schema(t, db); after != before {
		t.Fatalf("second run changed the schema:\n%s\n---\n%s", before, after)
	}
}
