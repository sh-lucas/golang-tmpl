// Package blobs contains helpers shared by the small and large blob stores.
package blobs

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const (
	SmallLimit = 2 << 20
	LargeLimit = 100 << 20
)

var ErrTooLarge = errors.New("blob exceeds size limit")

// NewKey creates a UUIDv7 filename with extension as its suffix.
func NewKey(extension string) (string, error) {
	extension = strings.TrimPrefix(extension, ".")
	if extension == "" || strings.ContainsAny(extension, `/\\`) {
		return "", errors.New("blob extension must be a filename suffix")
	}
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generate blob UUIDv7: %w", err)
	}
	return id.String() + "." + extension, nil
}

// CopyToTemp streams source to a temporary file and enforces limit without
// materializing the source in memory.
func CopyToTemp(directory string, source io.Reader, limit int64) (string, error) {
	file, err := os.CreateTemp(directory, ".blob-*")
	if err != nil {
		return "", fmt.Errorf("create temporary blob: %w", err)
	}
	path := file.Name()
	success := false
	defer func() {
		if !success {
			_ = os.Remove(path)
		}
	}()

	written, copyErr := io.Copy(file, io.LimitReader(source, limit+1))
	closeErr := file.Close()
	if copyErr != nil {
		return "", fmt.Errorf("copy blob: %w", copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close temporary blob: %w", closeErr)
	}
	if written > limit {
		return "", fmt.Errorf("%w: limit is %d bytes", ErrTooLarge, limit)
	}
	success = true
	return path, nil
}

// Directory returns the directory for large blob files below databaseRoot.
func Directory(databaseRoot string) (string, error) {
	if databaseRoot == "" {
		return "", errors.New("large blobs require a filesystem database root")
	}
	return filepath.Join(databaseRoot, "large_blobs"), nil
}

// Path returns the safe on-disk location for a generated blob key.
func Path(directory, key string) (string, error) {
	if key == "" || filepath.Base(key) != key {
		return "", errors.New("invalid blob key")
	}
	return filepath.Join(directory, key), nil
}
