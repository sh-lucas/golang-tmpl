package admins_test

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rox-projects/golang-tmpl/internal/database"
	"github.com/rox-projects/golang-tmpl/internal/features/admins"
	"github.com/rox-projects/golang-tmpl/queries"
)

func TestAdminAuthenticationFlow(t *testing.T) {
	db, err := database.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mux := http.NewServeMux()
	admins.RegisterRoutes(mux, queries.New(db))

	created := request(t, mux, http.MethodPost, "/admins", `{"email":"admin@example.com","password":"correct horse battery staple"}`, "")
	if created.Code != http.StatusCreated {
		t.Fatalf("create admin: status=%d body=%s", created.Code, created.Body.String())
	}

	duplicate := request(t, mux, http.MethodPost, "/admins", `{"email":"other@example.com","password":"correct horse battery staple"}`, "")
	if duplicate.Code != http.StatusUnauthorized {
		t.Fatalf("unprotected second admin: status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}

	invalid := request(t, mux, http.MethodPost, "/auth/login", `{"email":"admin@example.com","password":"wrong password"}`, "")
	if invalid.Code != http.StatusUnauthorized {
		t.Fatalf("invalid login: status=%d body=%s", invalid.Code, invalid.Body.String())
	}

	login := request(t, mux, http.MethodPost, "/auth/login", `{"email":"admin@example.com","password":"correct horse battery staple"}`, "")
	if login.Code != http.StatusOK {
		t.Fatalf("login: status=%d body=%s", login.Code, login.Body.String())
	}
	var session struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &session, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if session.Token == "" {
		t.Fatal("login returned an empty token")
	}

	me := request(t, mux, http.MethodGet, "/admins/me", "", session.Token)
	if me.Code != http.StatusOK || !bytes.Contains(me.Body.Bytes(), []byte(`"email":"admin@example.com"`)) {
		t.Fatalf("me: status=%d body=%s", me.Code, me.Body.String())
	}

	second := request(t, mux, http.MethodPost, "/admins", `{"email":"other@example.com","password":"another secure password"}`, session.Token)
	if second.Code != http.StatusCreated {
		t.Fatalf("authenticated admin creation: status=%d body=%s", second.Code, second.Body.String())
	}
}

func TestAdminRequestsRejectInvalidJSON(t *testing.T) {
	db, err := database.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mux := http.NewServeMux()
	admins.RegisterRoutes(mux, queries.New(db))
	response := request(t, mux, http.MethodPost, "/admins", `{"email":"admin@example.com","password":"long enough password","extra":true}`, "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown JSON field: status=%d body=%s", response.Code, response.Body.String())
	}
}

func request(t *testing.T, handler http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}
