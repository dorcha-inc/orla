package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/harvard-cns/orla/internal/storage"
	"github.com/harvard-cns/orla/internal/telemetry"
)

func freshPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping storage-backed test in -short mode")
	}
	ctx := context.Background()

	pgC, err := postgres.Run(ctx,
		"postgres:17",
		postgres.WithDatabase("orla"),
		postgres.WithUsername("orla"),
		postgres.WithPassword("orla"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pgC.Terminate(context.Background()) })

	dsn, err := pgC.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	store, err := storage.Open(ctx, storage.OpenConfig{DatabaseURL: dsn})
	require.NoError(t, err)
	t.Cleanup(store.Close)
	return store.Pool()
}

func waitForFlush(t *testing.T, w *telemetry.CompletionWriter, want int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if w.Flushes() >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected flushes >= %d, got %d", want, w.Flushes())
}

func TestCompletionWriter_RoundTrip(t *testing.T) {
	pool := freshPool(t)
	w := telemetry.NewCompletionWriter(telemetry.CompletionWriterConfig{
		Pool:      pool,
		BatchSize: 2,
		Interval:  50 * time.Millisecond,
	})
	t.Cleanup(func() { _ = w.Close(context.Background()) })

	rec := &telemetry.CompletionRecord{
		CompletionID:     "chatcmpl-abc",
		StageID:          "planning",
		WorkflowRun:      "wf-1",
		Backend:          "gpt4o",
		Status:           "success",
		PromptTokens:     new(10),
		CompletionTokens: new(20),
		LatencyMs:        new(150),
		CostUSD:          new(0.0012),
		Tags:             map[string]string{"tenant": "alice"},
		CreatedAt:        time.Now(),
	}
	require.True(t, w.Submit(rec))

	waitForFlush(t, w, 1)

	var count int
	row := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM completion_records WHERE completion_id = $1`,
		"chatcmpl-abc")
	require.NoError(t, row.Scan(&count))
	assert.Equal(t, 1, count)

	var workflowRun *string
	var promptTokens *int32
	var latencyMs *int32
	var costUSD *float64
	var status string
	var tags []byte
	row = pool.QueryRow(context.Background(),
		`SELECT workflow_run, prompt_tokens, latency_ms, cost_usd, status, tags
		 FROM completion_records WHERE completion_id = $1`,
		"chatcmpl-abc")
	require.NoError(t, row.Scan(&workflowRun, &promptTokens, &latencyMs, &costUSD, &status, &tags))

	require.NotNil(t, workflowRun)
	assert.Equal(t, "wf-1", *workflowRun)
	require.NotNil(t, promptTokens)
	assert.Equal(t, int32(10), *promptTokens)
	require.NotNil(t, latencyMs)
	assert.Equal(t, int32(150), *latencyMs)
	require.NotNil(t, costUSD)
	assert.InDelta(t, 0.0012, *costUSD, 1e-9)
	assert.Equal(t, "success", status)
	assert.Contains(t, string(tags), `"tenant"`)
}

func TestReader_CostByMapping(t *testing.T) {
	pool := freshPool(t)
	w := telemetry.NewCompletionWriter(telemetry.CompletionWriterConfig{
		Pool:      pool,
		BatchSize: 10,
		Interval:  50 * time.Millisecond,
	})
	t.Cleanup(func() { _ = w.Close(context.Background()) })

	// Two dispatches on the live critical path (empty mapping) and one
	// on a shadow variant.
	recs := []*telemetry.CompletionRecord{
		{CompletionID: "c1", StageID: "planning", Backend: "live", Status: "success", CostUSD: new(0.01), CreatedAt: time.Now()},
		{CompletionID: "c2", StageID: "planning", Backend: "live", Status: "success", CostUSD: new(0.02), CreatedAt: time.Now()},
		{CompletionID: "c3", StageID: "planning", Backend: "shadow", Status: "success", CostUSD: new(0.05), Mapping: "cand", CreatedAt: time.Now()},
	}
	for _, rec := range recs {
		require.True(t, w.Submit(rec))
	}
	waitForFlush(t, w, 1)

	reader := telemetry.NewReader(pool)
	costs, err := reader.CostByMapping(context.Background(), time.Time{})
	require.NoError(t, err)

	byMapping := make(map[string]*telemetry.MappingCost, len(costs))
	for _, c := range costs {
		byMapping[c.Mapping] = c
	}
	require.Contains(t, byMapping, "")
	require.Contains(t, byMapping, "cand")
	assert.Equal(t, int64(2), byMapping[""].Count)
	assert.InDelta(t, 0.03, byMapping[""].TotalCostUSD, 1e-9)
	assert.Equal(t, int64(1), byMapping["cand"].Count)
	assert.InDelta(t, 0.05, byMapping["cand"].TotalCostUSD, 1e-9)
}

func TestCompletionWriter_NullableColumnsNullWhenEmpty(t *testing.T) {
	pool := freshPool(t)
	w := telemetry.NewCompletionWriter(telemetry.CompletionWriterConfig{
		Pool:      pool,
		BatchSize: 1,
		Interval:  10 * time.Millisecond,
	})
	t.Cleanup(func() { _ = w.Close(context.Background()) })

	rec := &telemetry.CompletionRecord{
		CompletionID: "chatcmpl-min",
		StageID:      "planning",
		Backend:      "gpt4o",
		Status:       "error",
		// no workflow_run, no tokens, no cost
		CreatedAt: time.Now(),
	}
	require.True(t, w.Submit(rec))
	waitForFlush(t, w, 1)

	var workflowRun *string
	var pt, ct, lm *int32
	var cost *float64
	row := pool.QueryRow(context.Background(),
		`SELECT workflow_run, prompt_tokens, completion_tokens, latency_ms, cost_usd
		 FROM completion_records WHERE completion_id = $1`, "chatcmpl-min")
	require.NoError(t, row.Scan(&workflowRun, &pt, &ct, &lm, &cost))
	assert.Nil(t, workflowRun)
	assert.Nil(t, pt)
	assert.Nil(t, ct)
	assert.Nil(t, lm)
	assert.Nil(t, cost)
}

func TestCompletionWriter_BatchBySize(t *testing.T) {
	pool := freshPool(t)
	w := telemetry.NewCompletionWriter(telemetry.CompletionWriterConfig{
		Pool:      pool,
		BatchSize: 3,
		Interval:  10 * time.Second, // disable time-based flush
	})
	t.Cleanup(func() { _ = w.Close(context.Background()) })

	for i := range 3 {
		w.Submit(&telemetry.CompletionRecord{
			CompletionID: "id-" + string(rune('a'+i)),
			StageID:      "s", Backend: "b", Status: "success",
			CreatedAt: time.Now(),
		})
	}
	waitForFlush(t, w, 1)

	var n int
	row := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM completion_records`)
	require.NoError(t, row.Scan(&n))
	assert.Equal(t, 3, n)
}

func TestCompletionWriter_CloseDrains(t *testing.T) {
	pool := freshPool(t)
	w := telemetry.NewCompletionWriter(telemetry.CompletionWriterConfig{
		Pool:      pool,
		BatchSize: 100,         // large batch
		Interval:  time.Minute, // disable time-based flush
	})

	w.Submit(&telemetry.CompletionRecord{
		CompletionID: "drain", StageID: "s", Backend: "b", Status: "success",
		CreatedAt: time.Now(),
	})
	// Close must run the final flush before returning.
	require.NoError(t, w.Close(context.Background()))

	var n int
	row := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM completion_records WHERE completion_id = 'drain'`)
	require.NoError(t, row.Scan(&n))
	assert.Equal(t, 1, n)
}
