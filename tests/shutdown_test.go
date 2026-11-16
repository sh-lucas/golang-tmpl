//go:build integration

package tests

import (
	"context"
	json "encoding/json/v2"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rox-projects/golang-tmpl/database"
)

// Keep drainTimeout in sync with cmd/backend; shutdownTimeout is the limit we
// promise (below the 10s docker stop grace period).
const (
	drainTimeout    = 5 * time.Second
	shutdownTimeout = 8 * time.Second
)

func waitUntilListenerCloses(t *testing.T, a *app) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", a.addr, 100*time.Millisecond)
		if err != nil {
			return
		}
		_ = conn.Close()
		if time.Now().After(deadline) {
			t.Fatal("server still accepts connections after the shutdown signal")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestShutdownDrainsInFlightRequestsAndLeavesACleanDatabase(t *testing.T) {
	a := startBackend(t)
	a.createAdmin(t, "admin@example.com")
	login := holdRequest(t, a, http.MethodPost, "/auth/login", "", `{"email":"admin@example.com","password":"correct horse battery staple"}`)

	a.signal(t, syscall.SIGTERM)
	waitUntilListenerCloses(t, a)
	// The in-flight login still completes, including its database write.
	if response := login.finish(t); response.StatusCode != http.StatusOK {
		t.Fatalf("in-flight login status = %d", response.StatusCode)
	}

	err, took := a.wait(t, shutdownTimeout)
	if code := exitCode(err); code != 0 {
		t.Fatalf("exit code = %d (%v)", code, err)
	}
	if took > 2*time.Second {
		t.Fatalf("idle shutdown took %s", took)
	}
	path := filepath.Join(a.root, "sqlite.db")
	assertNoSidecars(t, path)

	db, err := database.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var admins, sessions int
	if err := db.QueryRow("SELECT (SELECT count(*) FROM admins), (SELECT count(*) FROM admin_sessions)").Scan(&admins, &sessions); err != nil {
		t.Fatal(err)
	}
	if admins != 1 || sessions != 1 {
		t.Fatalf("after restart: admins=%d sessions=%d, want 1 and 1", admins, sessions)
	}

	// Logs are one JSON object per line so collectors (e.g. Loki) can parse them.
	output := strings.TrimSpace(a.stderr.String())
	for _, line := range strings.Split(output, "\n") {
		var entry struct {
			Level string `json:"level"`
			Msg   string `json:"msg"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil || entry.Level == "" || entry.Msg == "" {
			t.Fatalf("log line is not structured JSON: %q (%v)", line, err)
		}
	}
	if !strings.Contains(output, `"msg":"shutting down"`) {
		t.Fatalf("shutdown was not logged:\n%s", output)
	}
}

func TestShutdownGivesUpOnStuckRequestsAfterTheDrainTimeout(t *testing.T) {
	a := startBackend(t)
	a.createAdmin(t, "admin@example.com")
	// The body never arrives, so this handler would block until ReadTimeout.
	holdRequest(t, a, http.MethodPost, "/auth/login", "", `{"email":"admin@example.com","password":"x"}`)

	a.signal(t, syscall.SIGTERM)
	err, took := a.wait(t, shutdownTimeout+time.Second)
	if code := exitCode(err); code != 0 {
		t.Fatalf("exit code = %d (%v)", code, err)
	}
	if took < drainTimeout-time.Second {
		t.Fatalf("exited after %s, before waiting for the in-flight request", took)
	}
	assertNoSidecars(t, filepath.Join(a.root, "sqlite.db"))
}

func TestSecondSignalForcesExit(t *testing.T) {
	a := startBackend(t)
	a.createAdmin(t, "admin@example.com")
	holdRequest(t, a, http.MethodPost, "/auth/login", "", `{"email":"admin@example.com","password":"x"}`)

	a.signal(t, syscall.SIGTERM)
	waitUntilListenerCloses(t, a)
	a.signal(t, syscall.SIGINT)
	err, _ := a.wait(t, time.Second)
	if code := exitCode(err); code == 0 {
		t.Fatal("forced exit reported success")
	}
}

// Browsers preconnect sockets and HTTP clients sometimes dial a connection they
// never use. net/http only treats such connections as idle after 5s, which
// would stall every shutdown.
func TestShutdownDoesNotWaitForConnectionsThatNeverSentARequest(t *testing.T) {
	a := startBackend(t)
	preconnected, err := net.Dial("tcp", a.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer preconnected.Close()
	// Connections are accepted in order, so once a later connection is served
	// the preconnected one has been accepted too.
	response, err := http.Get(a.url("/swagger.json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()

	a.signal(t, syscall.SIGTERM)
	err, took := a.wait(t, shutdownTimeout)
	if code := exitCode(err); code != 0 {
		t.Fatalf("exit code = %d (%v)", code, err)
	}
	if took > 2*time.Second {
		t.Fatalf("shutdown waited %s for a connection that never sent a request", took)
	}
}
