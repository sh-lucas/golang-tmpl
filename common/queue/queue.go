// Package queue provides a lightweight SQLite-backed JSON queue.
package queue

import (
	"context"
	databaseSQL "database/sql"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/rox-projects/golang-tmpl/queries"
)

const (
	batchSize  = 50
	queryDelay = 100 * time.Millisecond
)

// Payload embeds the typed JSON value so Item promotes its Data field.
type Payload[T any] struct {
	Data T
}

// Item is a decoded queue entry. Data contains the JSON payload supplied to
// Enqueue. The remaining fields identify the database row and its lifecycle.
type Item[T any] struct {
	Payload[T]
	ID               int64
	Reference        string
	EnqueuedAt       string
	UnavailableUntil string
	ProcessedAt      databaseSQL.NullString
	Err              error

	queue *Queue[T]
	ctx   context.Context
}

// Done marks the item as successfully processed.
func (i *Item[T]) Done() error {
	if i == nil || i.queue == nil {
		return errors.New("queue item is detached")
	}
	return i.queue.queries.MarkQueueDone(i.ctx, i.ID)
}

// Retry makes the item available again after delay, for example "5s".
func (i *Item[T]) Retry(delay string) error {
	if i == nil || i.queue == nil {
		return errors.New("queue item is detached")
	}
	seconds, err := durationSeconds(delay)
	if err != nil {
		return err
	}
	return i.queue.queries.RetryQueue(i.ctx, queries.RetryQueueParams{
		Column1: databaseSQL.NullString{String: seconds, Valid: true},
		ID:      i.ID,
	})
}

// Dead removes the item from normal processing permanently. The row remains
// available for inspection, but its far-future timestamp prevents claims.
func (i *Item[T]) Dead() error {
	if i == nil || i.queue == nil {
		return errors.New("queue item is detached")
	}
	return i.queue.queries.MarkQueueDead(i.ctx, i.ID)
}

func durationSeconds(value string) (string, error) {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return "", fmt.Errorf("invalid retry duration %q: %w", value, err)
	}
	if duration < 0 {
		return "", errors.New("retry duration must not be negative")
	}
	return strconv.FormatFloat(duration.Seconds(), 'f', -1, 64), nil
}

// Queue is a typed view over a SQLite queue. DBTX makes both *sql.DB and
// *sql.Tx suitable dependencies.
type Queue[T any] struct {
	queries *queries.Queries
}

// New creates a typed queue backed by db.
func New[T any](db queries.DBTX) *Queue[T] {
	return &Queue[T]{queries: queries.New(db)}
}

// Enqueue serializes payload as JSON and appends it to reference.
func (q *Queue[T]) Enqueue(ctx context.Context, reference string, payload T) (Item[T], error) {
	if q == nil || q.queries == nil {
		return Item[T]{}, errors.New("queue is nil")
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return Item[T]{}, fmt.Errorf("marshal queue payload: %w", err)
	}
	row, err := q.queries.Enqueue(ctx, queries.EnqueueParams{Data: data, Reference: reference})
	if err != nil {
		return Item[T]{}, fmt.Errorf("enqueue item: %w", err)
	}
	item := q.item(ctx, row)
	item.Data = payload
	return item, nil
}

// NewIterator creates a polling iterator. visibilitySeconds is the claim
// lease: an uncompleted item becomes eligible again when the lease expires.
func (q *Queue[T]) NewIterator(ctx context.Context, reference string, visibilitySeconds int) *Iterator[T] {
	return newIterator(ctx, q, reference, visibilitySeconds)
}

// Iterator polls one reference and emits up to 50 claimed items per query.
// Items starts its goroutine lazily and closes the returned channel on context
// cancellation or a database error. Check Err after the channel is closed.
type Iterator[T any] struct {
	ctx               context.Context
	queue             *Queue[T]
	reference         string
	visibilitySeconds int

	once  sync.Once
	items chan Item[T]
	mu    sync.Mutex
	err   error
}

// NewIterator creates an iterator directly from an injectable DBTX.
func NewIterator[T any](ctx context.Context, db queries.DBTX, reference string, visibilitySeconds int) *Iterator[T] {
	return newIterator(ctx, New[T](db), reference, visibilitySeconds)
}

func newIterator[T any](ctx context.Context, q *Queue[T], reference string, visibilitySeconds int) *Iterator[T] {
	if ctx == nil {
		ctx = context.Background()
	}
	if visibilitySeconds < 0 {
		visibilitySeconds = 0
	}
	return &Iterator[T]{
		ctx:               ctx,
		queue:             q,
		reference:         reference,
		visibilitySeconds: visibilitySeconds,
		items:             make(chan Item[T]),
	}
}

// Items starts and returns the iterator's output channel.
func (it *Iterator[T]) Items() <-chan Item[T] {
	if it == nil {
		closed := make(chan Item[T])
		close(closed)
		return closed
	}
	it.once.Do(func() { go it.run() })
	return it.items
}

// Run is an alias for Items, convenient for callers that name the operation.
func (it *Iterator[T]) Run() <-chan Item[T] { return it.Items() }

// Err returns the database error, if polling stopped because of one.
func (it *Iterator[T]) Err() error {
	if it == nil {
		return errors.New("iterator is nil")
	}
	it.mu.Lock()
	defer it.mu.Unlock()
	return it.err
}

func (it *Iterator[T]) run() {
	defer close(it.items)
	var queried bool
	for {
		if queried && !wait(it.ctx, queryDelay) {
			return
		}
		queried = true

		rows, err := it.queue.queries.ClaimBatch(it.ctx, queries.ClaimBatchParams{
			Column1:   strconv.Itoa(it.visibilitySeconds),
			Reference: it.reference,
		})
		if err != nil {
			if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				it.setErr(err)
			}
			return
		}
		sort.SliceStable(rows, func(a, b int) bool {
			if rows[a].EnqueuedAt == rows[b].EnqueuedAt {
				return rows[a].ID < rows[b].ID
			}
			return rows[a].EnqueuedAt < rows[b].EnqueuedAt
		})
		for _, row := range rows {
			item := it.queue.item(it.ctx, row)
			if err := json.Unmarshal(row.Data, &item.Data); err != nil {
				item.Err = fmt.Errorf("unmarshal queue item %d: %w", row.ID, err)
			}
			select {
			case it.items <- item:
			case <-it.ctx.Done():
				return
			}
		}
	}
}

func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func (it *Iterator[T]) setErr(err error) {
	it.mu.Lock()
	it.err = err
	it.mu.Unlock()
}

func (q *Queue[T]) item(ctx context.Context, row queries.Queue) Item[T] {
	return Item[T]{
		ID:               row.ID,
		Reference:        row.Reference,
		EnqueuedAt:       row.EnqueuedAt,
		UnavailableUntil: row.UnavailableUntil,
		ProcessedAt:      row.ProcessedAt,
		queue:            q,
		ctx:              ctx,
	}
}

// Enqueue serializes payload as JSON and appends it to reference using db.
func Enqueue[T any](ctx context.Context, db queries.DBTX, reference string, payload T) (Item[T], error) {
	return New[T](db).Enqueue(ctx, reference, payload)
}

// Iterate creates an iterator directly from db.
func Iterate[T any](ctx context.Context, db queries.DBTX, reference string, visibilitySeconds int) *Iterator[T] {
	return NewIterator[T](ctx, db, reference, visibilitySeconds)
}
