// Package mug contains small, explicit adapters for JSON HTTP endpoints.
package mug

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

const maxBodySize = 1 << 20

// Response is the HTTP result returned by an endpoint handler.
// Implementations can write headers, a status and a body as needed.
type Response interface {
	WriteHTTP(http.ResponseWriter)
}

// Handler contains endpoint logic after request decoding and validation.
type Handler[T any] func(context.Context, T) Response

// JSONResponse writes body as JSON with the supplied status code.
type JSONResponse struct {
	Status int
	Body   any
}

func (r JSONResponse) WriteHTTP(w http.ResponseWriter) {
	body, err := json.Marshal(r.Body)
	if err != nil {
		InternalServerError("could not encode response").WriteHTTP(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(r.Status)
	_, _ = w.Write(body)
}

// JSON creates a JSON response with an explicit status code.
func JSON(status int, body any) Response { return JSONResponse{Status: status, Body: body} }

func OK(body any) Response                 { return JSON(http.StatusOK, body) }
func Created(body any) Response            { return JSON(http.StatusCreated, body) }
func BadRequest(message string) Response   { return problem(http.StatusBadRequest, message) }
func Unauthorized(message string) Response { return problem(http.StatusUnauthorized, message) }
func NotFound(message string) Response     { return problem(http.StatusNotFound, message) }
func Conflict(message string) Response     { return problem(http.StatusConflict, message) }
func InternalServerError(message string) Response {
	return problem(http.StatusInternalServerError, message)
}

func problem(status int, message string) Response {
	return JSON(status, map[string]string{"error": message})
}

// Route adapts an endpoint handler to net/http. It accepts an optional JSON
// request body, rejects unknown fields, and validates it using validate tags.
func Route[T any](handler Handler[T]) http.Handler {
	validate := newValidator()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input T
		if response := decodeAndValidate(w, r, &input, validate); response != nil {
			response.WriteHTTP(w)
			return
		}
		handler(r.Context(), input).WriteHTTP(w)
	})
}

func decodeAndValidate(w http.ResponseWriter, r *http.Request, input any, validate *validator.Validate) Response {
	if r.Body != nil && r.Body != http.NoBody && r.ContentLength != 0 {
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			return JSON(http.StatusUnsupportedMediaType, map[string]string{"error": "content type must be application/json"})
		}
		err = json.UnmarshalRead(http.MaxBytesReader(w, r.Body, maxBodySize), input, json.RejectUnknownMembers(true))
		if err != nil && !errors.Is(err, io.EOF) {
			return BadRequest("invalid JSON body")
		}
	}
	if err := validate.Struct(input); err != nil {
		fields := map[string]string{}
		var validationErrors validator.ValidationErrors
		if errors.As(err, &validationErrors) {
			for _, field := range validationErrors {
				fields[field.Field()] = field.Tag()
			}
		}
		return JSON(http.StatusBadRequest, map[string]any{"error": "validation failed", "fields": fields})
	}
	return nil
}

func newValidator() *validator.Validate {
	validate := validator.New(validator.WithRequiredStructEnabled())
	validate.RegisterTagNameFunc(func(field reflect.StructField) string {
		name := strings.SplitN(field.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		return name
	})
	return validate
}
