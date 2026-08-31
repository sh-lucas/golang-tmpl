// Package small stores blobs up to 2 MiB directly in SQLite.
package small

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
	Key  string
	Meta string
	Data []byte
}

// Create streams source, verifies its 2 MiB limit, and stores it in SQLite.
func Create(ctx context.Context, db *sql.DB, meta, extension string, source io.Reader) (Blob, error) {
	key, err := blobs.NewKey(extension)
	if err != nil {
		return Blob{}, err
	}
	temporary, err := blobs.CopyToTemp("", source, blobs.SmallLimit)
	if err != nil {
		return Blob{}, err
	}
	defer os.Remove(temporary)
	data, err := os.ReadFile(temporary)
	if err != nil {
		return Blob{}, fmt.Errorf("read verified small blob: %w", err)
	}
	row, err := queries.New(db).CreateSmallBlob(ctx, queries.CreateSmallBlobParams{
		Key: key, Meta: meta, Data: data,
	})
	if err != nil {
		return Blob{}, fmt.Errorf("create small blob: %w", err)
	}
	return Blob{Key: row.Key, Meta: row.Meta, Data: row.Data}, nil
}

// Get returns a small blob's metadata and data.
func Get(ctx context.Context, db *sql.DB, key string) (Blob, error) {
	row, err := queries.New(db).GetSmallBlob(ctx, key)
	if err != nil {
		return Blob{}, fmt.Errorf("get small blob: %w", err)
	}
	return Blob{Key: row.Key, Meta: row.Meta, Data: row.Data}, nil
}

// Delete removes a small blob from SQLite.
func Delete(ctx context.Context, db *sql.DB, key string) error {
	if err := queries.New(db).DeleteSmallBlob(ctx, key); err != nil {
		return fmt.Errorf("delete small blob: %w", err)
	}
	return nil
}
