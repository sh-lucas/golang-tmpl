package helpers_test

import (
	"bytes"
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rox-projects/golang-tmpl/internal/helpers"
)

type input struct {
	Email string `json:"email" validate:"required,email"`
}

func TestHandleRequestDecodesValidatesAndResponds(t *testing.T) {
	h := helpers.NewHTTP()
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"email":"admin@example.com"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()

	request, ok := helpers.HandleRequest[input](h, r, w)
	if !ok {
		t.Fatalf("request rejected: %s", w.Body.String())
	}
	if request.Body.Email != "admin@example.com" || request.Authorization != "Bearer secret" {
		t.Fatalf("unexpected request: %#v", request)
	}
	request.Send(map[string]any{"ok": true}, http.StatusAccepted)
	if w.Code != http.StatusAccepted || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("response: status=%d content-type=%q", w.Code, w.Header().Get("Content-Type"))
	}
}

func TestHandleRequestReturnsValidationErrors(t *testing.T) {
	h := helpers.NewHTTP()
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"email":"invalid"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	if _, ok := helpers.HandleRequest[input](h, r, w); ok {
		t.Fatal("invalid request accepted")
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
}
