//go:build integration

// Package tests holds cross-feature tests. Files tagged "integration" run the
// real backend binary (main wiring, signals, Litestream) and are slow, so they
// only run on demand: just test-integration.
package tests

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

var backendBinary string

func TestMain(m *testing.M) {
	directory, err := os.MkdirTemp("", "golang-tmpl-tests-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	backendBinary = filepath.Join(directory, "backend")
	build := exec.Command("go", "build", "-o", backendBinary, "github.com/rox-projects/golang-tmpl/cmd/backend")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "build backend:", err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(directory)
	os.Exit(code)
}

const accessKey = "integration-access-key"

type app struct {
	root   string
	addr   string
	cmd    *exec.Cmd
	exited chan struct{}
	err    error
	stderr *lockedBuffer
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func freeAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().String()
}

// appEnv is the minimal environment the backend needs to start.
func appEnv(root, addr string) []string {
	_, port, _ := net.SplitHostPort(addr)
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"DATABASE_ROOT=" + root,
		"SERVER_PORT=" + port,
		"JWT_SECRET=integration-jwt-secret",
		"DATABASE_ACCESS_KEY=" + accessKey,
		"BOOK_ROOT=" + root,
	}
}

// start runs command (the backend, or a supervisor wrapping it) and waits until
// the HTTP server answers.
func start(t *testing.T, root, addr string, extraEnv []string, command string, args ...string) *app {
	t.Helper()
	a := &app{root: root, addr: addr, exited: make(chan struct{}), stderr: &lockedBuffer{}}
	a.cmd = exec.Command(command, args...)
	a.cmd.Env = append(appEnv(root, addr), extraEnv...)
	a.cmd.Stdout = a.stderr
	a.cmd.Stderr = a.stderr
	// Own process group, so cleanup also kills children (litestream -exec).
	a.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	a.cmd.WaitDelay = time.Second
	if err := a.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		a.err = a.cmd.Wait()
		close(a.exited)
	}()
	t.Cleanup(func() {
		_ = syscall.Kill(-a.cmd.Process.Pid, syscall.SIGKILL)
		<-a.exited
		if t.Failed() {
			t.Logf("process output:\n%s", a.stderr.String())
		}
	})
	deadline := time.Now().Add(15 * time.Second)
	for {
		response, err := http.Get(a.url("/swagger.json"))
		if err == nil {
			_ = response.Body.Close()
			return a
		}
		select {
		case <-a.exited:
			t.Fatalf("process exited before serving: %v", a.err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not start: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func startBackend(t *testing.T) *app {
	t.Helper()
	return start(t, t.TempDir(), freeAddr(t), nil, backendBinary)
}

func (a *app) url(path string) string { return "http://" + a.addr + path }

func (a *app) signal(t *testing.T, sig os.Signal) {
	t.Helper()
	if err := a.cmd.Process.Signal(sig); err != nil {
		t.Fatal(err)
	}
}

// wait returns the process exit error and how long it took to exit.
func (a *app) wait(t *testing.T, limit time.Duration) (error, time.Duration) {
	t.Helper()
	started := time.Now()
	select {
	case <-a.exited:
		return a.err, time.Since(started)
	case <-time.After(limit):
		t.Fatalf("process still running after %s", limit)
		return nil, 0
	}
}

func (a *app) createAdmin(t *testing.T, email string) {
	t.Helper()
	response, err := http.Post(a.url("/admins"), "application/json", strings.NewReader(`{"email":"`+email+`","password":"correct horse battery staple"}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create admin %s: status %d body %s", email, response.StatusCode, body)
	}
}

// heldRequest is a request whose handler is running and blocked reading the
// body. Expect: 100-continue makes the server tell us when the handler started.
type heldRequest struct {
	conn   net.Conn
	reader *bufio.Reader
	body   string
}

func holdRequest(t *testing.T, a *app, method, path, token, body string) *heldRequest {
	t.Helper()
	conn, err := net.Dial("tcp", a.addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	header := fmt.Sprintf("%s %s HTTP/1.1\r\nHost: test\r\nContent-Type: application/json\r\nContent-Length: %d\r\nExpect: 100-continue\r\n", method, path, len(body))
	if token != "" {
		header += "Authorization: Bearer " + token + "\r\n"
	}
	if _, err := io.WriteString(conn, header+"\r\n"); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	for _, want := range []string{"HTTP/1.1 100 Continue\r\n", "\r\n"} {
		line, err := reader.ReadString('\n')
		if err != nil || line != want {
			t.Fatalf("waiting for handler to start: got %q, %v", line, err)
		}
	}
	return &heldRequest{conn: conn, reader: reader, body: body}
}

func (h *heldRequest) finish(t *testing.T) *http.Response {
	t.Helper()
	if _, err := io.WriteString(h.conn, h.body); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(h.reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func exitCode(err error) int {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal())
		}
		return exit.ExitCode()
	}
	if err != nil {
		return -1
	}
	return 0
}

func assertNoSidecars(t *testing.T, database string) {
	t.Helper()
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Stat(database + suffix); !os.IsNotExist(err) {
			t.Fatalf("%s%s remains after shutdown: %v", filepath.Base(database), suffix, err)
		}
	}
}
