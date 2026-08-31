package blobs_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rox-projects/golang-tmpl/common/blobs"
	"github.com/rox-projects/golang-tmpl/common/blobs/large"
	"github.com/rox-projects/golang-tmpl/common/blobs/small"
	"github.com/rox-projects/golang-tmpl/internal/database"
)

func TestSmallBlobLifecycle(t *testing.T) {
	ctx := context.Background()
	db, _ := openDB(t)
	created, err := small.Create(ctx, db, "text/plain", "txt", strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := uuid.Parse(strings.TrimSuffix(created.Key, ".txt"))
	if err != nil || key.Version() != uuid.Version(7) {
		t.Fatalf("key=%q is not UUIDv7", created.Key)
	}
	if created.Meta != "text/plain" || string(created.Data) != "hello" {
		t.Fatalf("created=%+v", created)
	}
	got, err := small.Get(ctx, db, created.Key)
	if err != nil || string(got.Data) != "hello" {
		t.Fatalf("get=%+v, err=%v", got, err)
	}
	if err := small.Delete(ctx, db, created.Key); err != nil {
		t.Fatal(err)
	}
	if _, err := small.Get(ctx, db, created.Key); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("get deleted small blob error=%v", err)
	}
}

func TestSmallBlobRejectsOversizeStream(t *testing.T) {
	ctx := context.Background()
	db, _ := openDB(t)
	_, err := small.Create(ctx, db, "", "bin", &zeroReader{remaining: blobs.SmallLimit + 1})
	if !errors.Is(err, blobs.ErrTooLarge) {
		t.Fatalf("oversize error=%v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM blobs").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("stored %d oversize blobs", count)
	}
}

func TestLargeBlobLifecycleStreamsFile(t *testing.T) {
	ctx := context.Background()
	db, databaseRoot := openDB(t)
	created, err := large.Create(ctx, db, databaseRoot, "application/octet-stream", "bin", bytes.NewReader([]byte("large-data")))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(databaseRoot, "large_blobs", created.Key)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("large file not created: %v", err)
	}
	got, err := large.Get(ctx, db, databaseRoot, created.Key)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(got.Reader)
	closeErr := got.Reader.Close()
	if err != nil || closeErr != nil || string(data) != "large-data" {
		t.Fatalf("large data=%q read=%v close=%v", data, err, closeErr)
	}
	if err := large.Delete(ctx, db, databaseRoot, created.Key); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("large file still exists: %v", err)
	}
}

func TestCopyToTempChecksLimitWhileStreaming(t *testing.T) {
	path, err := blobs.CopyToTemp(t.TempDir(), &zeroReader{remaining: 6}, 5)
	if !errors.Is(err, blobs.ErrTooLarge) {
		t.Fatalf("oversize error=%v", err)
	}
	if path != "" {
		t.Fatalf("unexpected temporary path %q", path)
	}
}

func openDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	root := t.TempDir()
	db, err := database.Open(context.Background(), filepath.Join(root, "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, root
}

type zeroReader struct{ remaining int64 }

func (r *zeroReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	read := int64(len(p))
	if read > r.remaining {
		read = r.remaining
	}
	r.remaining -= read
	return int(read), nil
}
