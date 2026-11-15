package libsql_test

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	json "encoding/json/v2"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rox-projects/golang-tmpl/internal/database"
	"github.com/rox-projects/golang-tmpl/internal/features/libsql"
	"github.com/rox-projects/golang-tmpl/migrations"
)

const accessKey = "test-access-key"

type snapshotFixture struct {
	root   string
	db     *sql.DB
	server *httptest.Server
}

func newSnapshotFixture(t *testing.T) snapshotFixture {
	t.Helper()
	root := t.TempDir()
	db, err := database.Open(context.Background(), filepath.Join(root, "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handler := libsql.RegisterRoutes(mux, db, accessKey, root)
	server := httptest.NewServer(mux)
	t.Cleanup(func() {
		server.Close()
		_ = handler.Close()
		_ = db.Close()
	})
	return snapshotFixture{root: root, db: db, server: server}
}

func (f snapshotFixture) do(t *testing.T, method, token string, body io.Reader) (*http.Response, []byte) {
	t.Helper()
	request, err := http.NewRequest(method, f.server.URL+"/libsql/snapshot", body)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	content, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response, content
}

func (f snapshotFixture) emails(t *testing.T) []string {
	t.Helper()
	return emailsIn(t, f.db)
}

func emailsIn(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query("SELECT email FROM admins ORDER BY email")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var emails []string
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			t.Fatal(err)
		}
		emails = append(emails, email)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return emails
}

func insertAdmin(t *testing.T, db *sql.DB, email string) {
	t.Helper()
	if _, err := db.Exec("INSERT INTO admins (email, password_hash) VALUES (?, 'hash')", email); err != nil {
		t.Fatal(err)
	}
}

// openFile opens a standalone database file without running migrations.
func openFile(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// rootEntries lists DATABASE_ROOT so tests can assert that rejected or
// finished requests leave no temporary files behind.
func rootEntries(t *testing.T, root string) []string {
	t.Helper()
	var names []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != root {
			relative, _ := filepath.Rel(root, path)
			names = append(names, relative)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func TestSnapshotDownloadAndRestoreFlow(t *testing.T) {
	f := newSnapshotFixture(t)
	insertAdmin(t, f.db, "kept@example.com")
	insertAdmin(t, f.db, "deleted-later@example.com")

	for _, method := range []string{http.MethodGet, http.MethodPut} {
		for _, token := range []string{"", "wrong-key"} {
			response, _ := f.do(t, method, token, strings.NewReader("ignored"))
			if response.StatusCode != http.StatusUnauthorized {
				t.Fatalf("%s with token %q: status = %d, want 401", method, token, response.StatusCode)
			}
		}
	}

	response, snapshot := f.do(t, http.MethodGet, accessKey, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("download status = %d body=%s", response.StatusCode, snapshot)
	}
	if got := response.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if !bytes.HasPrefix(snapshot, []byte("SQLite format 3\x00")) {
		t.Fatal("download is not a SQLite database")
	}
	snapshotPath := filepath.Join(t.TempDir(), "downloaded.db")
	if err := os.WriteFile(snapshotPath, snapshot, 0o600); err != nil {
		t.Fatal(err)
	}
	// The downloaded file must be self-contained: no WAL needed to read it.
	if got := emailsIn(t, openFile(t, snapshotPath)); !slices.Equal(got, []string{"deleted-later@example.com", "kept@example.com"}) {
		t.Fatalf("snapshot admins = %v", got)
	}
	if got := rootEntries(t, f.root); slices.ContainsFunc(got, func(name string) bool { return !strings.HasPrefix(name, "sqlite.db") }) {
		t.Fatalf("download left files behind: %v", got)
	}

	if _, err := f.db.Exec("DELETE FROM admins WHERE email = 'deleted-later@example.com'"); err != nil {
		t.Fatal(err)
	}
	insertAdmin(t, f.db, "created-after-snapshot@example.com")

	response, body := f.do(t, http.MethodPut, accessKey, bytes.NewReader(snapshot))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("restore status = %d body=%s", response.StatusCode, body)
	}
	var restored struct {
		Backup string `json:"backup"`
	}
	if err := json.Unmarshal(body, &restored); err != nil {
		t.Fatal(err)
	}
	if got := f.emails(t); !slices.Equal(got, []string{"deleted-later@example.com", "kept@example.com"}) {
		t.Fatalf("admins after restore = %v", got)
	}
	// The state replaced by the restore is kept as an undo point.
	if !strings.HasPrefix(restored.Backup, "backups/pre-restore-") || !strings.HasSuffix(restored.Backup, ".db") {
		t.Fatalf("backup = %q", restored.Backup)
	}
	if got := emailsIn(t, openFile(t, filepath.Join(f.root, restored.Backup))); !slices.Equal(got, []string{"created-after-snapshot@example.com", "kept@example.com"}) {
		t.Fatalf("pre-restore backup admins = %v", got)
	}

	// The live database keeps working in WAL mode with the same pool.
	insertAdmin(t, f.db, "after-restore@example.com")
	var journalMode string
	if err := f.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal mode after restore = %q", journalMode)
	}
	for _, name := range rootEntries(t, f.root) {
		if !strings.HasPrefix(name, "sqlite.db") && !strings.HasPrefix(name, "backups") {
			t.Fatalf("restore left %s behind", name)
		}
	}
}

func TestSnapshotRestoreRejectsInvalidUploadsWithoutTouchingTheDatabase(t *testing.T) {
	f := newSnapshotFixture(t)
	insertAdmin(t, f.db, "live@example.com")

	sqliteFile := func(t *testing.T, statements ...string) []byte {
		t.Helper()
		path := filepath.Join(t.TempDir(), "upload.db")
		db := openFile(t, path)
		for _, statement := range statements {
			if _, err := db.Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return content
	}
	_, valid := f.do(t, http.MethodGet, accessKey, nil)
	corrupted := bytes.Clone(valid)
	// Overwrite the second page (the first table b-tree) with garbage.
	for i := 4096; i < 8192 && i < len(corrupted); i++ {
		corrupted[i] = 0xA5
	}

	cases := map[string]struct {
		upload []byte
		reason string
	}{
		"empty":                 {nil, "not a SQLite database"},
		"not sqlite":            {[]byte("definitely not a database"), "not a SQLite database"},
		"truncated sqlite":      {valid[:len(valid)/2], "malformed"},
		"corrupted sqlite":      {corrupted, "malformed"},
		"unrelated sqlite":      {sqliteFile(t, "CREATE TABLE unrelated (id INTEGER)"), "no schema_migrations table"},
		"no applied migrations": {sqliteFile(t, "CREATE TABLE schema_migrations (version TEXT PRIMARY KEY)"), "no applied migrations"},
		"migration from future": {sqliteFile(t, "CREATE TABLE schema_migrations (version TEXT PRIMARY KEY)", "INSERT INTO schema_migrations VALUES ('001_admins.sql'), ('999_future.sql')"), `\"999_future.sql\" is unknown`},
		"foreign key violation": {withStatements(t, valid, "INSERT INTO admin_sessions (token_hash, admin_id, expires_at) VALUES (X'01', 424242, '2999-01-01')"), "1 foreign key violations"},
	}
	before := rootEntries(t, f.root)
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			response, body := f.do(t, http.MethodPut, accessKey, bytes.NewReader(test.upload))
			if response.StatusCode != http.StatusUnprocessableEntity || !bytes.Contains(body, []byte(test.reason)) {
				t.Fatalf("status = %d body=%s", response.StatusCode, body)
			}
			if got := f.emails(t); !slices.Equal(got, []string{"live@example.com"}) {
				t.Fatalf("live admins changed to %v", got)
			}
			if got := rootEntries(t, f.root); !slices.Equal(got, before) {
				t.Fatalf("rejected upload changed DATABASE_ROOT: %v, want %v", got, before)
			}
		})
	}
}

// withStatements returns a copy of a database file after running statements on it.
func withStatements(t *testing.T, content []byte, statements ...string) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "modified.db")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	db := openFile(t, path)
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	modified, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return modified
}

func TestSnapshotRestoreAcceptsDifferentPageSize(t *testing.T) {
	f := newSnapshotFixture(t)
	insertAdmin(t, f.db, "live@example.com")

	path := filepath.Join(t.TempDir(), "small-pages.db")
	upload := openFile(t, path)
	if _, err := upload.Exec("PRAGMA page_size = 1024"); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Run(context.Background(), upload); err != nil {
		t.Fatal(err)
	}
	insertAdmin(t, upload, "uploaded@example.com")
	if err := upload.Close(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	response, body := f.do(t, http.MethodPut, accessKey, bytes.NewReader(content))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("restore status = %d body=%s", response.StatusCode, body)
	}
	if got := f.emails(t); !slices.Equal(got, []string{"uploaded@example.com"}) {
		t.Fatalf("admins after restore = %v", got)
	}
}

func TestSnapshotRestoreRejectsConcurrentRestore(t *testing.T) {
	f := newSnapshotFixture(t)
	conn, err := net.Dial("tcp", strings.TrimPrefix(f.server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Expect: 100-continue makes the server confirm that the first restore
	// handler is running (and reading its body) before the second one starts.
	if _, err := io.WriteString(conn, "PUT /libsql/snapshot HTTP/1.1\r\nHost: test\r\nAuthorization: Bearer "+accessKey+"\r\nContent-Length: 10\r\nExpect: 100-continue\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(status, "100 Continue") {
		t.Fatalf("first restore did not start: %q %v", status, err)
	}
	if blank, err := reader.ReadString('\n'); err != nil || blank != "\r\n" {
		t.Fatalf("interim response terminator = %q %v", blank, err)
	}

	response, body := f.do(t, http.MethodPut, accessKey, strings.NewReader("second"))
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("concurrent restore status = %d body=%s", response.StatusCode, body)
	}

	if _, err := io.WriteString(conn, "0123456789"); err != nil {
		t.Fatal(err)
	}
	first, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = first.Body.Close()
	if first.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("first restore status = %d", first.StatusCode)
	}
}

func TestSnapshotRestoreRejectsOversizedUploadBeforeReadingIt(t *testing.T) {
	f := newSnapshotFixture(t)
	conn, err := net.Dial("tcp", strings.TrimPrefix(f.server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// No body is sent: the server must answer from the header alone.
	if _, err := io.WriteString(conn, "PUT /libsql/snapshot HTTP/1.1\r\nHost: test\r\nAuthorization: Bearer "+accessKey+"\r\nContent-Length: 4294967297\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if got := rootEntries(t, f.root); slices.ContainsFunc(got, func(name string) bool { return !strings.HasPrefix(name, "sqlite.db") }) {
		t.Fatalf("oversized upload left files behind: %v", got)
	}
}
