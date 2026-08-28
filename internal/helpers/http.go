package helpers

import (
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

type HTTP struct{ validate *validator.Validate }

type Headers struct {
	Authorization string
}

type Response struct{ http.ResponseWriter }

type Request[T any] struct {
	Body T
	Headers
	Response
}

func NewHTTP() *HTTP {
	validate := validator.New(validator.WithRequiredStructEnabled())
	validate.RegisterTagNameFunc(func(field reflect.StructField) string {
		name := strings.SplitN(field.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		return name
	})
	return &HTTP{validate: validate}
}

func HandleRequest[T any](h *HTTP, r *http.Request, w http.ResponseWriter) (*Request[T], bool) {
	request := &Request[T]{
		Headers:  Headers{Authorization: r.Header.Get("Authorization")},
		Response: Response{ResponseWriter: w},
	}

	if r.Body != nil && r.ContentLength != 0 {
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			request.Error("content type must be application/json", http.StatusUnsupportedMediaType)
			return nil, false
		}
		err = json.UnmarshalRead(http.MaxBytesReader(w, r.Body, maxBodySize), &request.Body, json.RejectUnknownMembers(true))
		if err != nil && !errors.Is(err, io.EOF) {
			request.Error("invalid JSON body", http.StatusBadRequest)
			return nil, false
		}
	}

	if err := h.validate.Struct(request.Body); err != nil {
		fields := make(map[string]string)
		var validationErrors validator.ValidationErrors
		if errors.As(err, &validationErrors) {
			for _, field := range validationErrors {
				fields[field.Field()] = field.Tag()
			}
		}
		request.Send(map[string]any{"error": "validation failed", "fields": fields}, http.StatusBadRequest)
		return nil, false
	}
	return request, true
}

func (r Response) Send(body any, statusCode int) {
	r.Header().Set("Content-Type", "application/json")
	r.WriteHeader(statusCode)
	_ = json.MarshalWrite(r.ResponseWriter, body)
}

func (r Response) Error(message string, statusCode int) {
	r.Send(map[string]any{"error": message}, statusCode)
}
