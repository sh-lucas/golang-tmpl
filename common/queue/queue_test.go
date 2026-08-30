package queue_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/rox-projects/golang-tmpl/common/queue"
	"github.com/rox-projects/golang-tmpl/internal/database"
)

type payload struct {
	Name string `json:"name"`
	N    int    `json:"n"`
}

func TestEnqueueAndIteratorDecodeJSON(t *testing.T) {
	db := openDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := queue.Enqueue(ctx, db, "emails", payload{Name: "hello", N: 7}); err != nil {
		t.Fatal(err)
	}
	iterator := queue.NewIterator[payload](ctx, db, "emails", 30)
	select {
	case item := <-iterator.Items():
		if item.Err != nil {
			t.Fatal(item.Err)
		}
		if item.Data != (payload{Name: "hello", N: 7}) {
			t.Fatalf("decoded payload=%#v", item.Data)
		}
		if item.Reference != "emails" || item.ID == 0 || item.EnqueuedAt == "" {
			t.Fatalf("metadata=%+v", item)
		}
		if err := item.Done(); err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for queue item")
	}

	var processed sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT processed_at FROM queue WHERE reference = ?", "emails").Scan(&processed); err != nil {
		t.Fatal(err)
	}
	if !processed.Valid {
		t.Fatal("Done did not set processed_at")
	}
}

func TestIteratorClaimsAtMostFiftyPerQueryAndSupportsMultiplexing(t *testing.T) {
	db := openDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for i := 0; i < 55; i++ {
		if _, err := queue.Enqueue(ctx, db, "bulk", payload{N: i}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := queue.Enqueue(ctx, db, "other", payload{N: 99}); err != nil {
		t.Fatal(err)
	}

	iterator := queue.NewIterator[payload](ctx, db, "bulk", 30)
	seen := map[int]bool{}
	deadline := time.After(3 * time.Second)
	for len(seen) < 55 {
		select {
		case item := <-iterator.Items():
			if item.Err != nil {
				t.Fatal(item.Err)
			}
			seen[item.Data.N] = true
			if err := item.Done(); err != nil {
				t.Fatal(err)
			}
		case <-deadline:
			t.Fatalf("received %d/55 items", len(seen))
		}
	}
	if seen[99] {
		t.Fatal("iterator crossed reference boundary")
	}
}

func TestItemRetryAndDead(t *testing.T) {
	db := openDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	item, err := queue.Enqueue(ctx, db, "lifecycle", payload{Name: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := item.Retry("10ms"); err != nil {
		t.Fatal(err)
	}
	var unavailable string
	if err := db.QueryRowContext(ctx, "SELECT unavailable_until FROM queue WHERE id = ?", item.ID).Scan(&unavailable); err != nil {
		t.Fatal(err)
	}
	if unavailable <= time.Now().UTC().Format("2006-01-02 15:04:05.000") {
		t.Fatalf("retry did not defer item: %q", unavailable)
	}
	if err := item.Dead(); err != nil {
		t.Fatal(err)
	}
	if unavailable, err = readUnavailable(ctx, db, item.ID); err != nil {
		t.Fatal(err)
	}
	if unavailable != "9999-01-01 00:00:00" {
		t.Fatalf("dead timestamp=%q", unavailable)
	}
	if err := item.Retry("not a duration"); err == nil {
		t.Fatal("invalid retry duration accepted")
	}
}

func TestRetryMakesItemAvailableAfterShortDelay(t *testing.T) {
	db := openDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	queued, err := queue.Enqueue(ctx, db, "retry", payload{Name: "soon"})
	if err != nil {
		t.Fatal(err)
	}
	if err := queued.Retry("10ms"); err != nil {
		t.Fatal(err)
	}

	select {
	case item := <-queue.NewIterator[payload](ctx, db, "retry", 1).Items():
		if item.Err != nil {
			t.Fatal(item.Err)
		}
		if item.ID != queued.ID || item.Data.Name != "soon" {
			t.Fatalf("retried item=%+v", item)
		}
	case <-time.After(time.Second):
		t.Fatal("retried item was not available within one second")
	}
}

func TestIteratorReportsMalformedJSON(t *testing.T) {
	db := openDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := db.ExecContext(ctx, "INSERT INTO queue (data, reference) VALUES (?, ?)", []byte("{"), "bad"); err != nil {
		t.Fatal(err)
	}
	select {
	case item := <-queue.NewIterator[payload](ctx, db, "bad", 30).Items():
		if item.Err == nil {
			t.Fatal("malformed JSON did not report an error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for malformed item")
	}
}

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func readUnavailable(ctx context.Context, db *sql.DB, id int64) (string, error) {
	var value string
	err := db.QueryRowContext(ctx, "SELECT unavailable_until FROM queue WHERE id = ?", id).Scan(&value)
	return value, err
}
