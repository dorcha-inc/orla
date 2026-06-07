package metrics_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/harvard-cns/orla/internal/metrics"
	"github.com/harvard-cns/orla/internal/scheduler"
)

func TestMetrics_RequestsCounter(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := metrics.New(reg)

	m.RequestsTotal.WithLabelValues("planning", "gpt4o", "success").Inc()
	m.RequestsTotal.WithLabelValues("planning", "gpt4o", "success").Inc()
	m.RequestsTotal.WithLabelValues("planning", "gpt4o", "error").Inc()

	got := testutil.ToFloat64(m.RequestsTotal.WithLabelValues("planning", "gpt4o", "success"))
	assert.InDelta(t, 2.0, got, 1e-9)
}

func TestMetrics_BackendLatencyHistogram(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := metrics.New(reg)

	m.BackendLatency.WithLabelValues("gpt4o").Observe(0.1)
	m.BackendLatency.WithLabelValues("gpt4o").Observe(0.3)

	got := testutil.CollectAndCount(m.BackendLatency)
	assert.Equal(t, 1, got, "one label combination")
}

type fakeStatsSource struct{ stats []scheduler.Stats }

func (f *fakeStatsSource) Stats() []scheduler.Stats { return f.stats }

func TestSchedulerCollector_Emits(t *testing.T) {
	reg := prometheus.NewRegistry()
	src := &fakeStatsSource{stats: []scheduler.Stats{
		{Backend: "gpt4o", QueueDepth: 3, InFlight: 2, Capacity: 4, Dispatched: 100, CircuitState: "closed"},
		{Backend: "ollama", QueueDepth: 0, InFlight: 1, Capacity: 2, Dispatched: 5, CircuitState: "open"},
	}}
	c := metrics.NewSchedulerCollector(src)
	reg.MustRegister(c)

	out, err := testutil.GatherAndCount(reg,
		"orla_scheduler_queue_depth",
		"orla_scheduler_in_flight",
		"orla_scheduler_capacity",
		"orla_scheduler_dispatched_total",
		"orla_circuit_breaker_state",
	)
	require.NoError(t, err)
	// 2 backends × 4 scheduler metrics + 2 backends × 3 circuit states = 14 samples.
	assert.Equal(t, 14, out)
}

func TestSchedulerCollector_CircuitStateGauge(t *testing.T) {
	reg := prometheus.NewRegistry()
	src := &fakeStatsSource{stats: []scheduler.Stats{
		{Backend: "b", CircuitState: "open"},
	}}
	c := metrics.NewSchedulerCollector(src)
	reg.MustRegister(c)

	mfs, err := reg.Gather()
	require.NoError(t, err)

	var found map[string]float64
	for _, mf := range mfs {
		if mf.GetName() != "orla_circuit_breaker_state" {
			continue
		}
		found = make(map[string]float64)
		for _, m := range mf.GetMetric() {
			var stateLabel string
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "state" {
					stateLabel = lp.GetValue()
				}
			}
			found[stateLabel] = m.GetGauge().GetValue()
		}
	}
	require.NotNil(t, found, "orla_circuit_breaker_state metric not found")
	assert.Equal(t, 1.0, found["open"], "active state must be 1")
	assert.Equal(t, 0.0, found["closed"], "inactive state must be 0")
	assert.Equal(t, 0.0, found["half-open"], "inactive state must be 0")
}

type fakeBatchStats struct{ drops, flushes, failures int64 }

func (f *fakeBatchStats) Drops() int64    { return f.drops }
func (f *fakeBatchStats) Flushes() int64  { return f.flushes }
func (f *fakeBatchStats) Failures() int64 { return f.failures }

func TestBatchWriterCollector_Emits(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := metrics.NewBatchWriterCollector(map[string]metrics.BatchWriterStats{
		"completion_records": &fakeBatchStats{drops: 1, flushes: 50, failures: 2},
		"feedback":           &fakeBatchStats{drops: 0, flushes: 10},
	})
	reg.MustRegister(c)

	// 2 kinds × 3 metrics = 6 samples.
	count, err := testutil.GatherAndCount(reg,
		"orla_batch_writer_drops_total",
		"orla_batch_writer_flushes_total",
		"orla_batch_writer_failures_total",
	)
	require.NoError(t, err)
	assert.Equal(t, 6, count)
}
