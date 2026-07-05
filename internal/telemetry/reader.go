package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harvard-cns/orla/internal/storage/db"
)

// Reader is the read-side API for the mapper. It wraps the sqlc
// queries with friendly types (no pgtype, no []byte JSON) so the HTTP
// handlers can serialize results directly.
type Reader struct {
	queries *db.Queries
}

// NewReader constructs a Reader against the supplied pool.
func NewReader(pool *pgxpool.Pool) *Reader {
	return &Reader{queries: db.New(pool)}
}

// CompletionMetrics is one row of /api/v1/stages/{id}/metrics.
type CompletionMetrics struct {
	Backend      string  `json:"backend"`
	Count        int64   `json:"count"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	P50LatencyMs float64 `json:"p50_latency_ms"`
	P95LatencyMs float64 `json:"p95_latency_ms"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	ErrorCount   int64   `json:"error_count"`
}

// ListStageCompletions returns recent completion records for a stage.
// If since is the zero time, no time filter is applied. limit must
// be > 0.
func (r *Reader) ListStageCompletions(ctx context.Context, stageID string, since time.Time, limit int32) ([]*CompletionRecord, error) {
	rows, err := r.queries.ListStageCompletions(ctx, db.ListStageCompletionsParams{
		StageID:    stageID,
		Since:      sinceParam(since),
		LimitCount: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("telemetry: list completions: %w", err)
	}
	out := make([]*CompletionRecord, 0, len(rows))
	for _, row := range rows {
		rec, err := rowToCompletionRecord(row)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

// ListStageFeedback returns recent feedback rows for a stage.
func (r *Reader) ListStageFeedback(ctx context.Context, stageID string, since time.Time, limit int32) ([]*Feedback, error) {
	rows, err := r.queries.ListStageFeedback(ctx, db.ListStageFeedbackParams{
		StageID:    stageID,
		Since:      sinceParam(since),
		LimitCount: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("telemetry: list feedback: %w", err)
	}
	out := make([]*Feedback, 0, len(rows))
	for _, row := range rows {
		fb, err := rowToFeedback(row)
		if err != nil {
			return nil, err
		}
		out = append(out, fb)
	}
	return out, nil
}

// StageMetrics returns per-backend aggregates for a stage.
func (r *Reader) StageMetrics(ctx context.Context, stageID string, since time.Time) ([]*CompletionMetrics, error) {
	rows, err := r.queries.StageMetricsByBackend(ctx, db.StageMetricsByBackendParams{
		StageID: stageID,
		Since:   sinceParam(since),
	})
	if err != nil {
		return nil, fmt.Errorf("telemetry: stage metrics: %w", err)
	}
	out := make([]*CompletionMetrics, 0, len(rows))
	for _, row := range rows {
		out = append(out, &CompletionMetrics{
			Backend:      row.Backend,
			Count:        row.Count,
			AvgLatencyMs: row.AvgLatencyMs,
			P50LatencyMs: row.P50LatencyMs,
			P95LatencyMs: row.P95LatencyMs,
			TotalCostUSD: row.TotalCostUsd,
			ErrorCount:   row.ErrorCount,
		})
	}
	return out, nil
}

// MappingCost is one row of the cost-by-mapping aggregate. The live
// critical path aggregates under the empty Mapping, each shadow variant
// under its own name. Count is the dispatch count, not the example
// count, so callers that know how many examples they drove compute
// cost per example themselves.
type MappingCost struct {
	Mapping      string  `json:"mapping"`
	Count        int64   `json:"count"`
	TotalCostUSD float64 `json:"total_cost_usd"`
}

// CostByMapping returns total cost and dispatch count grouped by
// mapping variant. If since is the zero time, no time filter applies.
func (r *Reader) CostByMapping(ctx context.Context, since time.Time) ([]*MappingCost, error) {
	rows, err := r.queries.CostByMapping(ctx, sinceParam(since))
	if err != nil {
		return nil, fmt.Errorf("telemetry: cost by mapping: %w", err)
	}
	out := make([]*MappingCost, 0, len(rows))
	for _, row := range rows {
		out = append(out, &MappingCost{
			Mapping:      row.Mapping,
			Count:        row.Count,
			TotalCostUSD: row.TotalCostUsd,
		})
	}
	return out, nil
}

func sinceParam(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{Valid: false}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func rowToCompletionRecord(row db.ListStageCompletionsRow) (*CompletionRecord, error) {
	rec := &CompletionRecord{
		CompletionID: row.CompletionID,
		StageID:      row.StageID,
		Backend:      row.Backend,
		Status:       row.Status,
		CreatedAt:    row.CreatedAt.Time,
	}
	if row.WorkflowRun != nil {
		rec.WorkflowRun = *row.WorkflowRun
	}
	if row.PromptTokens != nil {
		v := int(*row.PromptTokens)
		rec.PromptTokens = &v
	}
	if row.CompletionTokens != nil {
		v := int(*row.CompletionTokens)
		rec.CompletionTokens = &v
	}
	if row.LatencyMs != nil {
		v := int(*row.LatencyMs)
		rec.LatencyMs = &v
	}
	if row.CostUsd != nil {
		v := *row.CostUsd
		rec.CostUSD = &v
	}
	if len(row.Usage) > 0 && string(row.Usage) != "{}" {
		var usage map[string]float64
		if err := json.Unmarshal(row.Usage, &usage); err != nil {
			return nil, fmt.Errorf("decode usage for completion %q: %w", row.CompletionID, err)
		}
		rec.Usage = usage
	}
	if row.ToolKind != nil {
		rec.ToolKind = *row.ToolKind
	}
	if len(row.Tags) > 0 && string(row.Tags) != "{}" {
		var tags map[string]string
		if err := json.Unmarshal(row.Tags, &tags); err != nil {
			return nil, fmt.Errorf("decode tags for completion %q: %w", row.CompletionID, err)
		}
		rec.Tags = tags
	}
	rec.Mapping = row.Mapping
	return rec, nil
}

func rowToFeedback(row db.Feedback) (*Feedback, error) {
	fb := &Feedback{
		ID:           row.ID,
		CompletionID: row.CompletionID,
		StageID:      row.StageID,
		Notes:        row.Notes,
		CreatedAt:    row.CreatedAt.Time,
	}
	if row.WorkflowRun != nil {
		fb.WorkflowRun = *row.WorkflowRun
	}
	if row.Rating != nil {
		v := *row.Rating
		fb.Rating = &v
	}
	if len(row.Labels) > 0 {
		var labels []string
		if err := json.Unmarshal(row.Labels, &labels); err == nil {
			fb.Labels = labels
		}
	}
	return fb, nil
}
