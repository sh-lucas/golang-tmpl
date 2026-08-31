// Package health provides the process health endpoint.
package health

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	StatusOK       = "ok"
	StatusUnstable = "unstable"
	StatusDown     = "down"

	heapLimitBytes       = 500 * 1024 * 1024
	cpuLimitPercent      = 50.0
	psiLimitPercent      = 30.0
	goroutineLimit       = 200
	googleRequestTimeout = 2 * time.Second
)

type Process struct {
	CPUUsagePercent float64 `json:"cpu_usage_percent"`
	MemoryRSSBytes  int64   `json:"memory_rss_bytes"`
	HeapBytes       uint64  `json:"heap_bytes"`
}

type Database struct {
	SizeBytes int64 `json:"size_bytes"`
}

// PSI contains the Linux PSI "some" avg10 values, as percentages.
type PSI struct {
	CPU    float64 `json:"cpu_avg10_percent"`
	IO     float64 `json:"io_avg10_percent"`
	Memory float64 `json:"memory_avg10_percent"`
}

type Response struct {
	Status          string   `json:"status"`
	Goroutines      int      `json:"goroutines"`
	GoogleLatencyMS float64  `json:"google_latency_ms,omitempty"`
	Process         Process  `json:"process"`
	Database        Database `json:"database"`
	PSI             *PSI     `json:"psi,omitempty"`
}

type Options struct {
	DatabasePath string
	GoogleURL    string
	HTTPClient   *http.Client
	ReadProcess  func() (Process, error)
	DatabaseSize func() (int64, error)
	ReadPSI      func() (*PSI, error)
}

type handler struct {
	options Options
}

func RegisterRoutes(mux *http.ServeMux, options Options) {
	if options.GoogleURL == "" {
		options.GoogleURL = "https://www.google.com/generate_204"
	}
	if options.HTTPClient == nil {
		options.HTTPClient = &http.Client{Timeout: googleRequestTimeout}
	}
	if options.ReadProcess == nil {
		options.ReadProcess = newProcessReader().read
	}
	if options.DatabaseSize == nil {
		options.DatabaseSize = func() (int64, error) {
			info, err := os.Stat(options.DatabasePath)
			if err != nil {
				return 0, fmt.Errorf("stat database: %w", err)
			}
			return info.Size(), nil
		}
	}
	if options.ReadPSI == nil {
		options.ReadPSI = readPSI
	}
	h := handler{options: options}
	mux.HandleFunc("GET /health", h.get)
}

// get godoc
// @Summary Get application health and process metrics
// @Tags health
// @Produce json
// @Success 200 {object} Response
// @Failure 503 {object} Response
// @Router /health [get]
func (h handler) get(w http.ResponseWriter, request *http.Request) {
	response := Response{Status: StatusOK, Goroutines: runtime.NumGoroutine()}
	failed := false
	if process, err := h.options.ReadProcess(); err != nil {
		failed = true
	} else {
		response.Process = process
	}
	if size, err := h.options.DatabaseSize(); err != nil {
		failed = true
	} else {
		response.Database.SizeBytes = size
	}
	if psi, err := h.options.ReadPSI(); err != nil {
		failed = true
	} else {
		response.PSI = psi
	}
	if latency, err := h.googleLatency(request.Context()); err != nil {
		failed = true
	} else {
		response.GoogleLatencyMS = float64(latency) / float64(time.Millisecond)
	}

	if failed {
		response.Status = StatusDown
	} else if response.Process.HeapBytes > heapLimitBytes || response.Process.CPUUsagePercent > cpuLimitPercent || maxPSI(response.PSI) > psiLimitPercent || response.Goroutines > goroutineLimit {
		response.Status = StatusUnstable
	}

	w.Header().Set("Content-Type", "application/json")
	if response.Status == StatusDown {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	if err := json.MarshalWrite(w, response); err != nil {
		return
	}
}

func (h handler) googleLatency(ctx context.Context) (time.Duration, error) {
	started := time.Now()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, h.options.GoogleURL, nil)
	if err != nil {
		return 0, err
	}
	response, err := h.options.HTTPClient.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return 0, fmt.Errorf("google probe returned HTTP %d", response.StatusCode)
	}
	return time.Since(started), nil
}

type processReader struct {
	mu          sync.Mutex
	initialized bool
	lastCPU     float64
	lastAt      time.Time
}

func newProcessReader() *processReader {
	return &processReader{}
}

func (r *processReader) read() (Process, error) {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return Process{}, fmt.Errorf("read process CPU usage: %w", err)
	}
	rss, err := readRSS()
	if err != nil {
		return Process{}, err
	}
	cpuSeconds := float64(usage.Utime.Sec+usage.Stime.Sec) + float64(usage.Utime.Usec+usage.Stime.Usec)/1e6
	r.mu.Lock()
	cpuUsagePercent := 0.0
	elapsed := 0.0
	if r.initialized {
		cpuUsagePercent = cpuSeconds - r.lastCPU
		elapsed = time.Since(r.lastAt).Seconds()
	}
	r.initialized = true
	r.lastCPU = cpuSeconds
	r.lastAt = time.Now()
	r.mu.Unlock()
	if elapsed > 0 {
		cpuUsagePercent = cpuUsagePercent / elapsed * 100
	} else {
		cpuUsagePercent = 0
	}
	return Process{
		CPUUsagePercent: cpuUsagePercent,
		MemoryRSSBytes:  rss,
		HeapBytes:       memory.HeapAlloc,
	}, nil
}

func readRSS() (int64, error) {
	content, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, fmt.Errorf("read process RSS: %w", err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "VmRSS:" && fields[2] == "kB" {
			kilobytes, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return 0, fmt.Errorf("parse process RSS: %w", err)
			}
			return kilobytes * 1024, nil
		}
	}
	return 0, errors.New("process RSS is unavailable")
}

func readPSI() (*PSI, error) {
	values := [3]float64{}
	paths := []string{"/proc/pressure/cpu", "/proc/pressure/io", "/proc/pressure/memory"}
	available := false
	for index, path := range paths {
		value, found, err := readPSIFile(path)
		if err != nil {
			return nil, err
		}
		if found {
			available = true
			values[index] = value
		}
	}
	if !available {
		return nil, nil
	}
	return &PSI{CPU: values[0], IO: values[1], Memory: values[2]}, nil
}

func readPSIFile(path string) (float64, bool, error) {
	content, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read PSI: %w", err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		if !strings.HasPrefix(line, "some ") {
			continue
		}
		for _, field := range strings.Fields(line) {
			value, found := strings.CutPrefix(field, "avg10=")
			if !found {
				continue
			}
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return 0, false, fmt.Errorf("parse PSI: %w", err)
			}
			return parsed, true, nil
		}
	}
	return 0, false, errors.New("PSI some avg10 is unavailable")
}

func maxPSI(psi *PSI) float64 {
	if psi == nil {
		return 0
	}
	return max(psi.CPU, psi.IO, psi.Memory)
}
