// Package libsql exposes the application SQLite database through the libSQL HTTP protocol.
package libsql

import (
	"database/sql"
	"net/http"

	libsqlhandler "github.com/sh-lucas/libsql-handler"
)

// RegisterRoutes adds the authenticated libSQL HTTP endpoint at /libsql and
// returns its handler so the caller can close its persistent sessions on shutdown.
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
func RegisterRoutes(mux *http.ServeMux, db *sql.DB, accessKey string) *libsqlhandler.LibSQLHandler {
	handler := libsqlhandler.New(db)
	endpoint := requireBearerAuth(accessKey, http.StripPrefix("/libsql", handler))
	mux.Handle("/libsql", endpoint)
	mux.Handle("/libsql/", endpoint)
	return handler
}
