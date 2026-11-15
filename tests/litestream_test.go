//go:build integration

package tests

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// These tests run the same command as the optional Litestream block in the
// Dockerfile, with a local file replica instead of S3.

func litestreamBinary(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("litestream")
	if err != nil {
		t.Skip("litestream is not installed")
	}
	return path
}

func replicateArgs() []string {
	return []string{"replicate", "-config", "../litestream.yml", "-exec", backendBinary}
}

func startReplicated(t *testing.T, root, replicaURL string) *app {
	t.Helper()
	return start(t, root, freeAddr(t), []string{"LITESTREAM_REPLICA_URL=" + replicaURL}, litestreamBinary(t), replicateArgs()...)
}

// waitForReplica waits until Litestream has written to the replica. Litestream
// picks up a brand-new database file on its first monitor tick (~1s); writes
// made and shut down before that are not replicated.
func waitForReplica(t *testing.T, directory string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		found := false
		_ = filepath.WalkDir(directory, func(_ string, entry os.DirEntry, err error) error {
			if err == nil && !entry.IsDir() {
				found = true
			}
			return nil
		})
		if found {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("litestream did not replicate the new database")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func stopReplicated(t *testing.T, a *app) {
	t.Helper()
	a.signal(t, syscall.SIGTERM)
	// Litestream forwards the signal, waits for the app, then syncs one last time.
	err, _ := a.wait(t, shutdownTimeout+5*time.Second)
	if code := exitCode(err); code != 0 {
		t.Fatalf("litestream exit code = %d (%v)", code, err)
	}
	if !strings.Contains(a.stderr.String(), `"msg":"shutting down"`) {
		t.Fatal("the app did not receive the shutdown signal through litestream")
	}
}

func authorized(t *testing.T, a *app, method, path, contentType string, body io.Reader) (int, []byte) {
	t.Helper()
	request, err := http.NewRequest(method, a.url(path), body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+accessKey)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	content, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, content
}

func loginStatus(t *testing.T, a *app, email string) int {
	t.Helper()
	response, err := http.Post(a.url("/auth/login"), "application/json", strings.NewReader(`{"email":"`+email+`","password":"correct horse battery staple"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	return response.StatusCode
}

func TestLitestreamReplicatesAndRestoresAFreshVolumeAutomatically(t *testing.T) {
	replicaDirectory := filepath.Join(t.TempDir(), "replica")
	replica := "file://" + replicaDirectory

	// First deploy: no backup yet, so the app starts with an empty database.
	first := startReplicated(t, t.TempDir(), replica)
	waitForReplica(t, replicaDirectory)
	first.createAdmin(t, "admin@example.com")

	// A snapshot restore through the API is replicated like any other write.
	status, snapshot := authorized(t, first, http.MethodGet, "/libsql/snapshot", "", nil)
	if status != http.StatusOK {
		t.Fatalf("download snapshot: %d", status)
	}
	if status, body := authorized(t, first, http.MethodPost, "/libsql", "application/json", strings.NewReader(`{"statements":["DELETE FROM admins"]}`)); status != http.StatusOK {
		t.Fatalf("delete admins: %d %s", status, body)
	}
	if got := loginStatus(t, first, "admin@example.com"); got != http.StatusUnauthorized {
		t.Fatalf("login after delete = %d", got)
	}
	if status, body := authorized(t, first, http.MethodPut, "/libsql/snapshot", "application/vnd.sqlite3", bytes.NewReader(snapshot)); status != http.StatusOK {
		t.Fatalf("restore snapshot: %d %s", status, body)
	}
	// Stopping right after the writes relies on Litestream's final sync.
	stopReplicated(t, first)

	// New, empty volume: the database comes back from the replica by itself.
	second := startReplicated(t, t.TempDir(), replica)
	if got := loginStatus(t, second, "admin@example.com"); got != http.StatusOK {
		t.Fatalf("login after automatic restore = %d", got)
	}
	stopReplicated(t, second)

	// An existing database is never overwritten by the replica.
	third := startReplicated(t, second.root, replica)
	if !strings.Contains(third.stderr.String(), "database exists, skipping restore") {
		t.Fatalf("existing database was not kept:\n%s", third.stderr.String())
	}
	stopReplicated(t, third)
}

func TestLitestreamRefusesToStartWhenTheReplicaIsUnusable(t *testing.T) {
	binary := litestreamBinary(t)
	unreadable := filepath.Join(t.TempDir(), "unreadable")
	if err := os.Mkdir(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o700) })

	cases := map[string]string{
		"missing replica URL": "",
		// Starting empty here would replicate an empty database over the backup.
		"unreadable replica": "file://" + unreadable,
	}
	for name, replica := range cases {
		t.Run(name, func(t *testing.T) {
			if name == "unreadable replica" && os.Geteuid() == 0 {
				t.Skip("root ignores directory permissions")
			}
			root := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, replicateArgs()...)
			cmd.Env = append(appEnv(root, freeAddr(t)), "LITESTREAM_REPLICA_URL="+replica)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
			cmd.WaitDelay = time.Second
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil || exitCode(err) == 0 {
				t.Fatalf("litestream did not refuse to start (err=%v):\n%s", err, output)
			}
			if bytes.Contains(output, []byte(`"msg":"listening"`)) {
				t.Fatalf("the app started:\n%s", output)
			}
			if _, err := os.Stat(filepath.Join(root, "sqlite.db")); !os.IsNotExist(err) {
				t.Fatalf("a database was created: %v", err)
			}
		})
	}
}
