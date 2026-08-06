// Package telemetry owns the data-plane writers (completion records,
// feedback) and the aggregation queries the mapper reads.
//
// Writes are async-batched via storage.BatchWriter. Reads (Phase 9)
// go through sqlc-generated queries against the same tables. The
// canonical sources are the .sql migrations under internal/storage/
// migrations and the sqlc queries under internal/storage/queries.
package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harvard-cns/orla/internal/storage"
)

// CompletionRecord is one row in the completion_records table.
// Pointers represent NULL-able columns.
//
// LLM dispatches populate PromptTokens and CompletionTokens. Tool
// dispatches populate Usage with the resources the tool reported, and
// ToolKind with the tool family. Both leave the other kind's fields
// empty. CostUSD is the final dollar amount, computed by the proxy at
// write time from token rates for LLMs or from Usage and Rates for
// tools.
type CompletionRecord struct {
	CompletionID     string             `json:"completion_id"`
	StageID          string             `json:"stage_id"`
	WorkflowRun      string             `json:"workflow_run,omitempty"`
	Backend          string             `json:"backend"`
	Status           string             `json:"status"`
	PromptTokens     *int               `json:"prompt_tokens,omitempty"`
	CompletionTokens *int               `json:"completion_tokens,omitempty"`
	CachedTokens     *int               `json:"cached_tokens,omitempty"`
	LatencyMs        *int               `json:"latency_ms,omitempty"`
	CostUSD          *float64           `json:"cost_usd,omitempty"`
	Usage            map[string]float64 `json:"usage,omitempty"`
	ToolKind         string             `json:"tool_kind,omitempty"`
	Tags             map[string]string  `json:"tags,omitempty"`
	Mapping          string             `json:"mapping,omitempty"`
	CreatedAt        time.Time          `json:"created_at"`

	// IO is non-nil only when the stage has capture_io on.
	IO *CapturedIO `json:"-"`
}

// CapturedIO is the request and response content the proxy captured for one
// completion. It is written to the separate completion_io table, keyed by the
// completion id and grouped by workflow run, never to completion_records.
type CapturedIO struct {
	Request  string
	Response string
}

// CompletionWriterConfig is the input to NewCompletionWriter.
type CompletionWriterConfig struct {
	Pool       *pgxpool.Pool
	Logger     *slog.Logger
	BufferSize int           // optional, default 4096
	BatchSize  int           // optional, default 200
	Interval   time.Duration // optional, default 200ms
}

// CompletionWriter is a typed wrapper over storage.BatchWriter for
// completion records. Submit is non-blocking, overflows are dropped
// and counted in Drops.
type CompletionWriter struct {
	bw      *storage.BatchWriter[*CompletionRecord]
	ioDrops atomic.Int64
}

// NewCompletionWriter starts a background flusher that uses pgx.CopyFrom
// to bulk-insert batches into the completion_records table.
func NewCompletionWriter(cfg CompletionWriterConfig) *CompletionWriter {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	w := &CompletionWriter{}
	w.bw = storage.NewBatchWriter(storage.BatchWriterConfig[*CompletionRecord]{
		Name:       "completion_records",
		BufferSize: cfg.BufferSize,
		BatchSize:  cfg.BatchSize,
		Interval:   cfg.Interval,
		Flush:      flushCompletions(cfg.Pool, cfg.Logger, &w.ioDrops),
		Logger:     cfg.Logger,
	})
	return w
}

// Submit enqueues a record. Returns false if the writer is closed or
// the buffer is full, either case is counted in Drops.
func (w *CompletionWriter) Submit(rec *CompletionRecord) bool {
	return w.bw.Submit(rec)
}

// Drops returns the cumulative count of dropped records.
func (w *CompletionWriter) Drops() int64 { return w.bw.Drops() }

// Flushes returns the cumulative count of successful flush batches.
func (w *CompletionWriter) Flushes() int64 { return w.bw.Flushes() }

// Failures returns the cumulative count of failed flush attempts.
func (w *CompletionWriter) Failures() int64 { return w.bw.Failures() }

// IODrops returns the cumulative count of captured completion_io rows lost
// to a failed best-effort write. Metadata writes are unaffected.
func (w *CompletionWriter) IODrops() int64 { return w.ioDrops.Load() }

// Close drains the buffer and waits for the final flush, bounded by ctx.
func (w *CompletionWriter) Close(ctx context.Context) error {
	return w.bw.Close(ctx)
}

func flushCompletions(pool *pgxpool.Pool, logger *slog.Logger, ioDrops *atomic.Int64) storage.FlushFunc[*CompletionRecord] {
	columns := []string{
		"completion_id", "stage_id", "workflow_run", "backend", "status",
		"prompt_tokens", "completion_tokens", "latency_ms", "cost_usd",
		"tags", "created_at", "usage", "tool_kind", "mapping", "cached_tokens",
	}
	return func(ctx context.Context, items []*CompletionRecord) error {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			return fmt.Errorf("acquire conn: %w", err)
		}
		defer conn.Release()

		rows := make([][]any, 0, len(items))
		for _, rec := range items {
			tagsBytes := encodeJSONBObject(rec.Tags, len(rec.Tags) == 0, logger, "tags", rec.CompletionID)
			usageBytes := encodeJSONBObject(rec.Usage, len(rec.Usage) == 0, logger, "usage", rec.CompletionID)
			rows = append(rows, []any{
				rec.CompletionID,
				rec.StageID,
				nullableString(rec.WorkflowRun),
				rec.Backend,
				rec.Status,
				intPtr(rec.PromptTokens),
				intPtr(rec.CompletionTokens),
				intPtr(rec.LatencyMs),
				rec.CostUSD,
				tagsBytes,
				rec.CreatedAt,
				usageBytes,
				nullableString(rec.ToolKind),
				rec.Mapping,
				intPtr(rec.CachedTokens),
			})
		}

		_, err = conn.Conn().CopyFrom(ctx,
			pgx.Identifier{"completion_records"},
			columns,
			pgx.CopyFromRows(rows),
		)
		if err != nil {
			return fmt.Errorf("copy completions: %w", err)
		}

		flushCompletionIO(ctx, conn.Conn(), items, logger, ioDrops)
		return nil
	}
}

var completionIOColumns = []string{
	"completion_id", "workflow_run", "stage_id",
	"request_content", "response_content", "created_at",
}

// flushCompletionIO writes captured request and response content into the
// separate completion_io table. It is best-effort. A failure here logs and
// returns without touching the metadata write, which has already committed.
func flushCompletionIO(ctx context.Context, conn *pgx.Conn, items []*CompletionRecord, logger *slog.Logger, ioDrops *atomic.Int64) {
	rows := make([][]any, 0)
	for _, rec := range items {
		if rec.IO == nil {
			continue
		}
		rows = append(rows, []any{
			rec.CompletionID,
			nullableString(rec.WorkflowRun),
			rec.StageID,
			nullableString(rec.IO.Request),
			nullableString(rec.IO.Response),
			rec.CreatedAt,
		})
	}
	if len(rows) == 0 {
		return
	}
	_, err := conn.CopyFrom(ctx,
		pgx.Identifier{"completion_io"},
		completionIOColumns,
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		ioDrops.Add(int64(len(rows)))
		if logger != nil {
			logger.Warn("telemetry: dropping captured completion_io batch",
				"rows", len(rows),
				"error", err.Error(),
			)
		}
	}
}

// jsonbEmptyObject is the JSONB sentinel for an empty object. Used as
// the default for NOT NULL JSONB columns when the source map is nil
// or fails to marshal.
var jsonbEmptyObject = []byte("{}")

// encodeJSONBObject marshals a value into JSONB bytes safe for a
// NOT NULL DEFAULT '{}'::jsonb column. Callers pass isEmpty
// pre-computed from the concrete map type so we avoid a reflective
// type switch. Marshal failures fall back to "{}" and log the
// column name and completion id so the loss is observable.
func encodeJSONBObject(v any, isEmpty bool, logger *slog.Logger, column, completionID string) []byte {
	if isEmpty {
		return jsonbEmptyObject
	}
	b, err := json.Marshal(v)
	if err != nil {
		if logger != nil {
			logger.Warn("telemetry: dropping JSONB column on marshal failure",
				"column", column,
				"completion_id", completionID,
				"error", err.Error(),
			)
		}
		return jsonbEmptyObject
	}
	return b
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// intPtr converts *int to int64 pointer for pgx (Postgres INTEGER takes
// int4, but []any with *int requires conversion).
func intPtr(p *int) any {
	if p == nil {
		return nil
	}
	v := int64(*p)
	return v
}
