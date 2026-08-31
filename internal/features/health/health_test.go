package health

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouteReportsOKWithMetrics(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, Options{
		GoogleURL: "http://google.test",
		HTTPClient: &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}, nil
		})},
		ReadProcess:  func() (Process, error) { return Process{CPUUsagePercent: 10, MemoryRSSBytes: 123, HeapBytes: 456}, nil },
		DatabaseSize: func() (int64, error) { return 789, nil },
		ReadPSI:      func() (*PSI, error) { return &PSI{CPU: 5, IO: 6, Memory: 7}, nil },
	})

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", recorder.Code, http.StatusOK)
	}
	var got Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusOK || got.GoogleLatencyMS < 0 || got.Process.HeapBytes != 456 || got.Database.SizeBytes != 789 || got.PSI.CPU != 5 {
		t.Fatalf("unexpected health response: %+v", got)
	}
}

func TestRouteReportsUnstableAtThresholds(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, Options{
		GoogleURL: "http://google.test", HTTPClient: &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
		})},
		ReadProcess:  func() (Process, error) { return Process{CPUUsagePercent: 50.1, HeapBytes: 1}, nil },
		DatabaseSize: func() (int64, error) { return 1, nil },
		ReadPSI:      func() (*PSI, error) { return nil, nil },
	})
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", recorder.Code, http.StatusOK)
	}
	var got Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusUnstable {
		t.Fatalf("status = %q, want %q", got.Status, StatusUnstable)
	}
}

func TestRouteReportsDownWhenGoogleFails(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, Options{
		GoogleURL: "http://google.test", HTTPClient: &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded })},
		ReadProcess:  func() (Process, error) { return Process{}, nil },
		DatabaseSize: func() (int64, error) { return 1, nil },
		ReadPSI:      func() (*PSI, error) { return nil, nil },
	})
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	var got Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusDown {
		t.Fatalf("status = %q, want %q", got.Status, StatusDown)
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
