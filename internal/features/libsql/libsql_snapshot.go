package libsql

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rox-projects/golang-tmpl/database"
	"github.com/rox-projects/golang-tmpl/database/migrations"
	"modernc.org/sqlite"
)

const (
	// Snapshot routes move whole databases, so they get longer deadlines than
	// the server-wide timeouts.
	snapshotTimeout  = 10 * time.Minute
	maxSnapshotBytes = 4 << 30
)

var errInvalidSnapshot = errors.New("invalid snapshot")

type snapshots struct {
	db        *sql.DB
	root      string
	restoring sync.Mutex
}

func extendDeadlines(w http.ResponseWriter) {
	controller := http.NewResponseController(w)
	deadline := time.Now().Add(snapshotTimeout)
	_ = controller.SetReadDeadline(deadline)
	_ = controller.SetWriteDeadline(deadline)
}

// vacuumInto writes a consistent, WAL-free copy of the live database to a new
// file in directory and returns its path.
func (s *snapshots) vacuumInto(ctx context.Context, directory, pattern string) (string, error) {
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return "", fmt.Errorf("create snapshot directory: %w", err)
	}
	file, err := os.CreateTemp(directory, pattern)
	if err != nil {
		return "", fmt.Errorf("create snapshot file: %w", err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close snapshot file: %w", err)
	}
	// VACUUM INTO accepts an existing empty file.
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", path); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("vacuum into snapshot: %w", err)
	}
	return path, nil
}

// backupCurrent keeps the database about to be replaced as an undo point and
// returns its path relative to the database root.
func (s *snapshots) backupCurrent(ctx context.Context) (string, error) {
	pattern := "pre-restore-" + time.Now().UTC().Format("20060102T150405.000Z") + "-*.db"
	path, err := s.vacuumInto(ctx, filepath.Join(s.root, "backups"), pattern)
	if err != nil {
		return "", err
	}
	return filepath.Rel(s.root, path)
}

// prepareSnapshot validates an uploaded database file, migrates it in place to
// this binary's schema and rewrites it with pageSize, because SQLite cannot
// restore into a WAL database with a different page size. The live database is
// never touched here.
func prepareSnapshot(ctx context.Context, path string, pageSize int) error {
	header := make([]byte, 16)
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	_, err = io.ReadFull(file, header)
	_ = file.Close()
	if err != nil || !bytes.Equal(header, []byte("SQLite format 3\x00")) {
		return fmt.Errorf("%w: not a SQLite database", errInvalidSnapshot)
	}

	if err := checkUpload(ctx, path); err != nil {
		return err
	}

	// Migrations run on the upload, so the restore itself is a single atomic copy.
	db, err := database.Open(ctx, path)
	if err != nil {
		return fmt.Errorf("%w: %w", errInvalidSnapshot, err)
	}
	defer db.Close()
	var violations int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM pragma_foreign_key_check").Scan(&violations); err != nil {
		return fmt.Errorf("%w: foreign key check: %w", errInvalidSnapshot, err)
	}
	if violations > 0 {
		return fmt.Errorf("%w: %d foreign key violations", errInvalidSnapshot, violations)
	}
	// Closing the only connection checkpoints the WAL into the file.
	if err := db.Close(); err != nil {
		return err
	}
	return setPageSize(ctx, path, pageSize)
}

func setPageSize(ctx context.Context, path string, pageSize int) error {
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		return err
	}
	defer db.Close()
	// Page size and journal mode are per-connection state, so pin one.
	db.SetMaxOpenConns(1)
	var current int
	if err := db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&current); err != nil {
		return fmt.Errorf("read upload page size: %w", err)
	}
	if current == pageSize {
		return nil
	}
	for _, statement := range []string{"PRAGMA journal_mode = DELETE", fmt.Sprintf("PRAGMA page_size = %d", pageSize), "VACUUM"} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("convert upload page size: %w", err)
		}
	}
	return db.Close()
}

// checkUpload runs read-only checks before anything writes to the upload.
func checkUpload(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=query_only(1)&_pragma=trusted_schema(0)")
	if err != nil {
		return err
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return fmt.Errorf("%w: integrity check: %w", errInvalidSnapshot, err)
	}
	var problems []string
	for rows.Next() {
		var problem string
		if err := rows.Scan(&problem); err != nil {
			_ = rows.Close()
			return fmt.Errorf("%w: integrity check: %w", errInvalidSnapshot, err)
		}
		problems = append(problems, problem)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("%w: integrity check: %w", errInvalidSnapshot, err)
	}
	if len(problems) != 1 || problems[0] != "ok" {
		return fmt.Errorf("%w: integrity check failed: %v", errInvalidSnapshot, problems)
	}

	// Without applied migrations the upload is not a database of this
	// application; restoring it would silently wipe everything.
	versions, err := db.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return fmt.Errorf("%w: no schema_migrations table: %w", errInvalidSnapshot, err)
	}
	defer versions.Close()
	applied := 0
	for versions.Next() {
		var version string
		if err := versions.Scan(&version); err != nil {
			return fmt.Errorf("%w: read schema_migrations: %w", errInvalidSnapshot, err)
		}
		if !migrations.Known(version) {
			return fmt.Errorf("%w: migration %q is unknown to this version", errInvalidSnapshot, version)
		}
		applied++
	}
	if err := versions.Err(); err != nil {
		return fmt.Errorf("%w: read schema_migrations: %w", errInvalidSnapshot, err)
	}
	if applied == 0 {
		return fmt.Errorf("%w: no applied migrations", errInvalidSnapshot)
	}
	return nil
}

// restoreFrom copies the prepared file over the live database with SQLite's
// online backup API. The copy runs in one write transaction, so other
// connections keep working and see either the old or the new database.
func (s *snapshots) restoreFrom(ctx context.Context, path string) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	return conn.Raw(func(driverConn any) error {
		restorer, ok := driverConn.(interface {
			NewRestore(string) (*sqlite.Backup, error)
		})
		if !ok {
			return errors.New("sqlite driver does not support online restore")
		}
		backup, err := restorer.NewRestore(path)
		if err != nil {
			return err
		}
		if _, err := backup.Step(-1); err != nil {
			_ = backup.Finish()
			return err
		}
		return backup.Finish()
	})
}

func removeDatabaseFiles(path string) {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		_ = os.Remove(path + suffix)
	}
}
