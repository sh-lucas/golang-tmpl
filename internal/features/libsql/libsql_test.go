package libsql_test

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rox-projects/golang-tmpl/internal/database"
	"github.com/rox-projects/golang-tmpl/internal/features/libsql"
)

func TestRouteRequiresBearerAuthenticationAndExecutesAgainstApplicationDatabase(t *testing.T) {
	db, err := database.Open(context.Background(), "file:libsql-route-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mux := http.NewServeMux()
	handler := libsql.RegisterRoutes(mux, db, "test-access-key")
	t.Cleanup(func() { _ = handler.Close() })

	unauthenticated := httptest.NewRecorder()
	unauthenticatedRequest := httptest.NewRequest(http.MethodPost, "/libsql", bytes.NewBufferString(`{"statements":["SELECT 1"]}`))
	mux.ServeHTTP(unauthenticated, unauthenticatedRequest)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", unauthenticated.Code, http.StatusUnauthorized)
	}
	if got := unauthenticated.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Fatalf("WWW-Authenticate = %q", got)
	}

	wrongToken := httptest.NewRecorder()
	wrongTokenRequest := httptest.NewRequest(http.MethodGet, "/libsql/version", nil)
	wrongTokenRequest.Header.Set("Authorization", "Bearer wrong-access-key")
	mux.ServeHTTP(wrongToken, wrongTokenRequest)
	if wrongToken.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d, want %d", wrongToken.Code, http.StatusUnauthorized)
	}

	version := httptest.NewRecorder()
	versionRequest := httptest.NewRequest(http.MethodGet, "/libsql/version", nil)
	versionRequest.Header.Set("Authorization", "Bearer test-access-key")
	mux.ServeHTTP(version, versionRequest)
	if version.Code != http.StatusOK || !bytes.HasPrefix(version.Body.Bytes(), []byte("sqld ")) {
		t.Fatalf("version response: status=%d body=%q", version.Code, version.Body.String())
	}

	execute := httptest.NewRecorder()
	executeRequest := httptest.NewRequest(http.MethodPost, "/libsql", bytes.NewBufferString(`{"statements":["CREATE TABLE libsql_route_test (value TEXT)",{"q":"INSERT INTO libsql_route_test (value) VALUES (?)","params":["shared database"]}]}`))
	executeRequest.Header.Set("Content-Type", "application/json")
	executeRequest.Header.Set("Authorization", "Bearer test-access-key")
	mux.ServeHTTP(execute, executeRequest)
	if execute.Code != http.StatusOK {
		t.Fatalf("execute status=%d body=%s", execute.Code, execute.Body.String())
	}
	var response []struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(execute.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response) != 2 || response[0].Error != "" || response[1].Error != "" {
		t.Fatalf("unexpected execution response: %s", execute.Body.String())
	}

	var value string
	if err := db.QueryRow("SELECT value FROM libsql_route_test").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "shared database" {
		t.Fatalf("database value = %q", value)
	}
}
