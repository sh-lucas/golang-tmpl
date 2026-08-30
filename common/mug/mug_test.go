package mug_test

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rox-projects/golang-tmpl/common/mug"
)

type createInput struct {
	Email string `json:"email" validate:"required,email"`
}

func TestRouteDecodesValidatesAndWritesResponse(t *testing.T) {
	handler := mug.Route(func(_ context.Context, input createInput) mug.Response {
		return mug.Created(map[string]string{"email": input.Email})
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"email":"admin@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	if res.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content-type=%q", res.Header().Get("Content-Type"))
	}
	var body map[string]string
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["email"] != "admin@example.com" {
		t.Fatalf("body=%#v", body)
	}
}

func TestRouteRejectsInvalidInputBeforeCallingHandler(t *testing.T) {
	called := false
	handler := mug.Route(func(_ context.Context, _ createInput) mug.Response {
		called = true
		return mug.OK(nil)
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"email":"invalid","extra":true}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if called {
		t.Fatal("handler called for invalid input")
	}
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestResponseConstructors(t *testing.T) {
	for _, response := range []struct {
		name string
		got  mug.Response
		want int
	}{
		{"ok", mug.OK(nil), http.StatusOK},
		{"not found", mug.NotFound("missing"), http.StatusNotFound},
		{"unauthorized", mug.Unauthorized("required"), http.StatusUnauthorized},
	} {
		t.Run(response.name, func(t *testing.T) {
			res := httptest.NewRecorder()
			response.got.WriteHTTP(res)
			if res.Code != response.want {
				t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
			}
		})
	}
}
