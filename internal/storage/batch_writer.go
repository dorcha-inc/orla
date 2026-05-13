package storage

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// BatchWriterConfig configures a BatchWriter.
type BatchWriterConfig struct {
	// Buffer is the channel capacity. Submissions past capacity are dropped
	// rather than blocking the caller.
	Buffer int
	// Batch is the maximum number of items collected before a flush.
	Batch int
	// Period is the maximum time a batch may sit before a flush.
	Period time.Duration
}

// BatchWriter accepts items via a non-blocking channel and flushes them in
// batches to a user-supplied function. It is generic so the same plumbing
// serves completion records, feedback, and any future append-only stream.
type BatchWriter[T any] struct {
	name   string
	cfg    BatchWriterConfig
	ch     chan T
	flush  func(ctx context.Context, batch []T) error
	onDrop func()

	startOnce sync.Once
	stop      chan struct{}
	done      chan struct{}
}

// NewBatchWriter constructs a BatchWriter. name is used in log lines.
// flush is called with each batch; if it returns an error, the batch is logged
// and discarded (we don't block the producer on storage errors).
// onDrop is called once per dropped item when the buffer is full; pass nil to
// disable. Typically a Prometheus counter Inc().
func NewBatchWriter[T any](name string, cfg BatchWriterConfig, flush func(ctx context.Context, batch []T) error, onDrop func()) *BatchWriter[T] {
	if cfg.Buffer <= 0 {
		cfg.Buffer = 1024
	}
	if cfg.Batch <= 0 {
		cfg.Batch = 100
	}
	if cfg.Period <= 0 {
		cfg.Period = 100 * time.Millisecond
	}
	return &BatchWriter[T]{
		name:   name,
		cfg:    cfg,
		ch:     make(chan T, cfg.Buffer),
		flush:  flush,
		onDrop: onDrop,
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
}

// Record enqueues an item. Non-blocking: if the buffer is full, the item is
// dropped and onDrop is called (if configured).
func (w *BatchWriter[T]) Record(item T) {
	select {
	case w.ch <- item:
	default:
		if w.onDrop != nil {
			w.onDrop()
		}
	}
}

// Start launches the background goroutine. It is safe to call once; subsequent
// calls are no-ops. The goroutine exits when ctx is cancelled or Stop is called.
func (w *BatchWriter[T]) Start(ctx context.Context) {
	w.startOnce.Do(func() { go w.run(ctx) })
}

// Stop signals the writer to drain remaining items and exit. It blocks until
// the background goroutine has finished.
func (w *BatchWriter[T]) Stop() {
	close(w.stop)
	<-w.done
}

func (w *BatchWriter[T]) run(ctx context.Context) {
	defer close(w.done)
	buf := make([]T, 0, w.cfg.Batch)
	ticker := time.NewTicker(w.cfg.Period)
	defer ticker.Stop()

	flush := func() {
		if len(buf) == 0 {
			return
		}
		if err := w.flush(ctx, buf); err != nil {
			zap.L().Error("batch writer flush failed",
				zap.String("name", w.name),
				zap.Int("batch_size", len(buf)),
				zap.Error(err))
		}
		buf = buf[:0]
	}

	for {
		select {
		case <-ctx.Done():
			// Drain anything already in the channel before exiting.
			for {
				select {
				case item := <-w.ch:
					buf = append(buf, item)
				default:
					flush()
					return
				}
			}
		case <-w.stop:
			for {
				select {
				case item := <-w.ch:
					buf = append(buf, item)
				default:
					flush()
					return
				}
			}
		case <-ticker.C:
			flush()
		case item := <-w.ch:
			buf = append(buf, item)
			if len(buf) >= w.cfg.Batch {
				flush()
			}
		}
	}
}
