// Package large stores blobs up to 100 MiB as files next to the SQLite DB.
package large

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"

	"github.com/rox-projects/golang-tmpl/common/blobs"
	"github.com/rox-projects/golang-tmpl/queries"
)

type Blob struct {
	Key    string
	Meta   string
	Reader io.ReadCloser
}

// Create streams source into <database directory>/blobs and records metadata.
func Create(ctx context.Context, db *sql.DB, databaseURI, meta, extension string, source io.Reader) (Blob, error) {
	directory, err := blobs.Directory(databaseURI)
	if err != nil {
		return Blob{}, err
	}
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return Blob{}, fmt.Errorf("create blob directory: %w", err)
	}
	key, err := blobs.NewKey(extension)
	if err != nil {
		return Blob{}, err
	}
	temporary, err := blobs.CopyToTemp(directory, source, blobs.LargeLimit)
	if err != nil {
		return Blob{}, err
	}
	defer os.Remove(temporary)
	path, err := blobs.Path(directory, key)
	if err != nil {
		return Blob{}, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Blob{}, fmt.Errorf("begin large blob transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	row, err := queries.New(tx).CreateLargeBlob(ctx, queries.CreateLargeBlobParams{Key: key, Meta: meta})
	if err != nil {
		return Blob{}, fmt.Errorf("create large blob metadata: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return Blob{}, fmt.Errorf("place large blob: %w", err)
	}
	if err := tx.Commit(); err != nil {
		_ = os.Remove(path)
		return Blob{}, fmt.Errorf("commit large blob: %w", err)
	}
	committed = true
	return Blob{Key: row.Key, Meta: row.Meta}, nil
}

// Get opens a large blob for streaming. The caller must close Reader.
func Get(ctx context.Context, db *sql.DB, databaseURI, key string) (Blob, error) {
	directory, err := blobs.Directory(databaseURI)
	if err != nil {
		return Blob{}, err
	}
	row, err := queries.New(db).GetLargeBlob(ctx, key)
	if err != nil {
		return Blob{}, fmt.Errorf("get large blob metadata: %w", err)
	}
	path, err := blobs.Path(directory, row.Key)
	if err != nil {
		return Blob{}, err
	}
	reader, err := os.Open(path)
	if err != nil {
		return Blob{}, fmt.Errorf("open large blob: %w", err)
	}
	return Blob{Key: row.Key, Meta: row.Meta, Reader: reader}, nil
}

// Delete removes the metadata and then the associated large blob file.
func Delete(ctx context.Context, db *sql.DB, databaseURI, key string) error {
	directory, err := blobs.Directory(databaseURI)
	if err != nil {
		return err
	}
	rowKey, err := queries.New(db).DeleteLargeBlob(ctx, key)
	if err != nil {
		return fmt.Errorf("delete large blob metadata: %w", err)
	}
	path, err := blobs.Path(directory, rowKey)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete large blob file: %w", err)
	}
	return nil
}
