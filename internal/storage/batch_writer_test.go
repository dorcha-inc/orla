package storage

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBatchWriter_FlushesOnBatchSize(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		mu       sync.Mutex
		received [][]int
	)
	w := NewBatchWriter[int]("test", BatchWriterConfig{
		Buffer: 100,
		Batch:  3,
		Period: time.Hour, // disable ticker, force batch-size triggering
	}, func(_ context.Context, batch []int) error {
		mu.Lock()
		defer mu.Unlock()
		copyBatch := append([]int(nil), batch...)
		received = append(received, copyBatch)
		return nil
	}, nil)
	w.Start(ctx)
	t.Cleanup(w.Stop)

	for i := 1; i <= 5; i++ {
		w.Record(i)
	}

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(received) >= 1 && len(received[0]) == 3
	}, time.Second, 10*time.Millisecond)

	mu.Lock()
	first := append([]int(nil), received[0]...)
	mu.Unlock()
	assert.Equal(t, []int{1, 2, 3}, first)
}

func TestBatchWriter_FlushesOnTick(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	flushed := make(chan []int, 4)
	w := NewBatchWriter[int]("test", BatchWriterConfig{
		Buffer: 100,
		Batch:  1000,
		Period: 30 * time.Millisecond,
	}, func(_ context.Context, batch []int) error {
		copyBatch := append([]int(nil), batch...)
		flushed <- copyBatch
		return nil
	}, nil)
	w.Start(ctx)
	t.Cleanup(w.Stop)

	w.Record(7)

	select {
	case batch := <-flushed:
		assert.Equal(t, []int{7}, batch)
	case <-time.After(time.Second):
		t.Fatal("ticker did not trigger flush")
	}
}

func TestBatchWriter_DropsWhenBufferFull(t *testing.T) {
	var dropCount atomic.Int64
	w := NewBatchWriter[int]("test", BatchWriterConfig{
		Buffer: 2,
		Batch:  100,
		Period: time.Hour,
	}, func(_ context.Context, _ []int) error { return nil },
		func() { dropCount.Add(1) },
	)
	// Intentionally do NOT call Start so the buffer fills up.
	w.Record(1)
	w.Record(2)
	w.Record(3) // should drop
	w.Record(4) // should drop

	assert.Equal(t, int64(2), dropCount.Load())
}

func TestBatchWriter_StopDrainsRemaining(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		mu       sync.Mutex
		received []int
	)
	w := NewBatchWriter[int]("test", BatchWriterConfig{
		Buffer: 100,
		Batch:  100,
		Period: time.Hour, // never tick during test
	}, func(_ context.Context, batch []int) error {
		mu.Lock()
		defer mu.Unlock()
		received = append(received, batch...)
		return nil
	}, nil)
	w.Start(ctx)

	w.Record(1)
	w.Record(2)
	w.Record(3)

	w.Stop()

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []int{1, 2, 3}, received, "Stop should drain the channel before exiting")
}
