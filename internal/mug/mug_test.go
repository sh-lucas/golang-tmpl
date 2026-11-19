package mug_test

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rox-projects/golang-tmpl/internal/mug"
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

type paramsInput struct {
	ID     int64  `path:"id" json:"-"`
	Limit  int8   `query:"limit" json:"-" validate:"max=50"`
	Active bool   `query:"active" json:"-"`
	Key    string `header:"x-api-key" json:"-" validate:"required"`
	Token  string `cookie:"session" json:"-"`
	Note   string `json:"note"`
}

// serveParams routes through a real ServeMux so PathValue is populated.
func serveParams(t *testing.T, target, body string, edit func(*http.Request)) (*httptest.ResponseRecorder, *paramsInput) {
	t.Helper()
	var got *paramsInput
	mux := http.NewServeMux()
	mux.Handle("POST /orders/{id}", mug.Route(func(_ context.Context, input paramsInput) mug.Response {
		got = &input
		return mug.OK(nil)
	}))
	req := httptest.NewRequest(http.MethodPost, target, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Api-Key", "secret")
	if edit != nil {
		edit(req)
	}
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	return res, got
}

func validationFields(t *testing.T, res *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	var body struct {
		Error  string            `json:"error"`
		Fields map[string]string `json:"fields"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error != "validation failed" {
		t.Fatalf("error=%q", body.Error)
	}
	return body.Fields
}

func TestRouteFillsParamsFromRequest(t *testing.T) {
	res, got := serveParams(t, "/orders/42?limit=7&active=true", `{"note":"hi"}`, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: "session", Value: "abc"})
	})
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	want := paramsInput{ID: 42, Limit: 7, Active: true, Key: "secret", Token: "abc", Note: "hi"}
	if *got != want {
		t.Fatalf("input=%+v want %+v", *got, want)
	}
}

func TestRouteLeavesMissingParamsZero(t *testing.T) {
	res, got := serveParams(t, "/orders/1?limit=", "", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	if want := (paramsInput{ID: 1, Key: "secret"}); *got != want {
		t.Fatalf("input=%+v want %+v", *got, want)
	}
}

func TestRouteRejectsInvalidParams(t *testing.T) {
	for _, tc := range []struct {
		name, target string
		edit         func(*http.Request)
		want         map[string]string
	}{
		{"path not a number", "/orders/abc", nil, map[string]string{"path.id": "int64"}},
		{"query overflows int8", "/orders/1?limit=300", nil, map[string]string{"query.limit": "int8"}},
		{"query not a bool", "/orders/1?active=maybe", nil, map[string]string{"query.active": "bool"}},
		{"several at once", "/orders/x?limit=y", nil, map[string]string{"path.id": "int64", "query.limit": "int8"}},
		{"validate tag on param", "/orders/1?limit=51", nil, map[string]string{"query.limit": "max"}},
		{"required header missing", "/orders/1", func(r *http.Request) { r.Header.Del("X-Api-Key") }, map[string]string{"header.x-api-key": "required"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, got := serveParams(t, tc.target, "", tc.edit)
			if got != nil {
				t.Fatal("handler called for invalid params")
			}
			fields := validationFields(t, res)
			if len(fields) != len(tc.want) {
				t.Fatalf("fields=%v want %v", fields, tc.want)
			}
			for key, tag := range tc.want {
				if fields[key] != tag {
					t.Fatalf("fields=%v want %v", fields, tc.want)
				}
			}
			if bytes.Contains(res.Body.Bytes(), []byte("abc")) {
				t.Fatalf("response echoes the raw value: %s", res.Body.String())
			}
		})
	}
}

func TestRouteBodyCannotSetParams(t *testing.T) {
	for _, body := range []string{`{"ID":7}`, `{"id":7}`, `{"Key":"forged"}`, `{"x-api-key":"forged"}`} {
		t.Run(body, func(t *testing.T) {
			res, got := serveParams(t, "/orders/1", body, nil)
			if got != nil || res.Code != http.StatusBadRequest {
				t.Fatalf("status=%d input=%+v body=%s", res.Code, got, res.Body.String())
			}
		})
	}
}

func TestRoutePanicsOnMisconfiguredParams(t *testing.T) {
	handler := func(context.Context, any) mug.Response { return mug.OK(nil) }
	for _, tc := range []struct {
		name  string
		build func()
	}{
		{"missing json dash", func() {
			type in struct {
				ID int `path:"id"`
			}
			mug.Route(func(ctx context.Context, i in) mug.Response { return handler(ctx, i) })
		}},
		{"unsupported type", func() {
			type in struct {
				Price float64 `query:"price" json:"-"`
			}
			mug.Route(func(ctx context.Context, i in) mug.Response { return handler(ctx, i) })
		}},
		{"two sources", func() {
			type in struct {
				ID string `path:"id" query:"id" json:"-"`
			}
			mug.Route(func(ctx context.Context, i in) mug.Response { return handler(ctx, i) })
		}},
		{"empty name", func() {
			type in struct {
				ID string `header:"" json:"-"`
			}
			mug.Route(func(ctx context.Context, i in) mug.Response { return handler(ctx, i) })
		}},
		{"unexported", func() {
			type in struct {
				id string `path:"id" json:"-"`
			}
			mug.Route(func(ctx context.Context, i in) mug.Response { return handler(ctx, i.id) })
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected panic when building the route")
				}
			}()
			tc.build()
		})
	}
}
