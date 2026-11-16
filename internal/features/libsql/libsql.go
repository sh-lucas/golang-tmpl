// Package libsql exposes the application SQLite database through the libSQL HTTP protocol.
package libsql

import (
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/rox-projects/golang-tmpl/internal/blobs"
	"github.com/rox-projects/golang-tmpl/internal/mug"

	libsqlhandler "github.com/sh-lucas/libsql-handler"
)

// RegisterRoutes adds the authenticated libSQL HTTP endpoint at /libsql, the
// snapshot routes at /libsql/snapshot, and returns the libSQL handler so the
// caller can close its persistent sessions on shutdown. databaseRoot is the
// directory holding the database; snapshots use it for temporary files and
// pre-restore backups.
//
// @Summary Execute SQL through libSQL HTTP
// @Description Connect using a Bearer token configured in DATABASE_ACCESS_KEY.
// @Tags libsql
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {array} map[string]any
// @Failure 401 {string} string
// @Router /libsql [post]
func RegisterRoutes(mux *http.ServeMux, db *sql.DB, accessKey, databaseRoot string) *libsqlhandler.LibSQLHandler {
	handler := libsqlhandler.New(db, libsqlhandler.WithLogger(slog.Default()))
	endpoint := requireBearerAuth(accessKey, http.StripPrefix("/libsql", handler))
	mux.Handle("/libsql", endpoint)
	mux.Handle("/libsql/", endpoint)
	s := &snapshots{db: db, root: databaseRoot}
	mux.Handle("GET /libsql/snapshot", requireBearerAuth(accessKey, http.HandlerFunc(s.download)))
	mux.Handle("PUT /libsql/snapshot", requireBearerAuth(accessKey, http.HandlerFunc(s.restore)))
	return handler
}

type restoreResponse struct {
	// Backup is the pre-restore copy of the replaced database, relative to DATABASE_ROOT.
	Backup string `json:"backup" example:"backups/pre-restore-20260101T000000.000Z.db"`
}

// download godoc
// @Summary Download a consistent database snapshot
// @Description Streams a self-contained copy of sqlite.db (VACUUM INTO). large_blobs/ is not included.
// @Tags libsql
// @Produce application/vnd.sqlite3
// @Security BearerAuth
// @Success 200 {file} file
// @Failure 401 {string} string
// @Router /libsql/snapshot [get]
func (s *snapshots) download(w http.ResponseWriter, r *http.Request) {
	extendDeadlines(w)
	path, err := s.vacuumInto(r.Context(), s.root, ".snapshot-*.db")
	if err != nil {
		slog.ErrorContext(r.Context(), "create snapshot", "error", err)
		mug.InternalServerError("could not create snapshot").WriteHTTP(w)
		return
	}
	defer os.Remove(path)
	file, err := os.Open(path)
	if err != nil {
		slog.ErrorContext(r.Context(), "open snapshot", "error", err)
		mug.InternalServerError("could not create snapshot").WriteHTTP(w)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		slog.ErrorContext(r.Context(), "stat snapshot", "error", err)
		mug.InternalServerError("could not create snapshot").WriteHTTP(w)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="sqlite.db"`)
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	if _, err := io.Copy(w, file); err != nil {
		slog.WarnContext(r.Context(), "send snapshot", "error", err)
	}
}

// restore godoc
// @Summary Replace the database with an uploaded snapshot
// @Description Validates the upload (integrity, foreign keys, known migrations), applies pending migrations to it, saves the current database under backups/ and restores the upload atomically. large_blobs/ is not touched.
// @Tags libsql
// @Accept application/vnd.sqlite3
// @Produce json
// @Security BearerAuth
// @Success 200 {object} restoreResponse
// @Failure 401 {string} string
// @Failure 409 {object} map[string]string
// @Failure 413 {object} map[string]string
// @Failure 422 {object} map[string]string
// @Router /libsql/snapshot [put]
func (s *snapshots) restore(w http.ResponseWriter, r *http.Request) {
	if !s.restoring.TryLock() {
		mug.Conflict("another restore is running").WriteHTTP(w)
		return
	}
	defer s.restoring.Unlock()
	if r.ContentLength > maxSnapshotBytes {
		mug.JSON(http.StatusRequestEntityTooLarge, map[string]string{"error": blobs.ErrTooLarge.Error()}).WriteHTTP(w)
		return
	}
	extendDeadlines(w)
	ctx := r.Context()

	upload, err := blobs.CopyToTemp(s.root, r.Body, maxSnapshotBytes)
	if errors.Is(err, blobs.ErrTooLarge) {
		mug.JSON(http.StatusRequestEntityTooLarge, map[string]string{"error": err.Error()}).WriteHTTP(w)
		return
	}
	if err != nil {
		mug.BadRequest("could not read upload").WriteHTTP(w)
		return
	}
	defer removeDatabaseFiles(upload)

	var pageSize int
	if err := s.db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		slog.ErrorContext(ctx, "read database page size", "error", err)
		mug.InternalServerError("could not read database page size").WriteHTTP(w)
		return
	}
	if err := prepareSnapshot(ctx, upload, pageSize); err != nil {
		if errors.Is(err, errInvalidSnapshot) {
			mug.JSON(http.StatusUnprocessableEntity, map[string]string{"error": err.Error()}).WriteHTTP(w)
			return
		}
		slog.ErrorContext(ctx, "prepare snapshot", "error", err)
		mug.InternalServerError("could not prepare snapshot").WriteHTTP(w)
		return
	}
	backup, err := s.backupCurrent(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "back up database before restore", "error", err)
		mug.InternalServerError("could not back up the current database; nothing was restored").WriteHTTP(w)
		return
	}
	if err := s.restoreFrom(ctx, upload); err != nil {
		slog.ErrorContext(ctx, "restore snapshot", "error", err, "backup", backup)
		mug.InternalServerError("restore failed; the database was not changed").WriteHTTP(w)
		return
	}
	slog.WarnContext(ctx, "database restored from snapshot", "backup", backup)
	mug.OK(restoreResponse{Backup: backup}).WriteHTTP(w)
}
