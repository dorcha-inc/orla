package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/harvard-cns/orla/internal/scheduler"
)

// SchedulerStatsSource is the subset of scheduler.Scheduler used by
// SchedulerCollector. The interface lets tests pass a fake.
type SchedulerStatsSource interface {
	Stats() []scheduler.Stats
}

// SchedulerCollector emits per-backend queue depth, in-flight count,
// capacity gauges, and circuit breaker state at scrape time.
type SchedulerCollector struct {
	src SchedulerStatsSource

	queueDepth   *prometheus.Desc
	inFlight     *prometheus.Desc
	capacity     *prometheus.Desc
	dispatched   *prometheus.Desc
	circuitState *prometheus.Desc
}

// NewSchedulerCollector constructs a collector. Caller registers it
// with prom via reg.MustRegister(c).
func NewSchedulerCollector(src SchedulerStatsSource) *SchedulerCollector {
	return &SchedulerCollector{
		src: src,
		queueDepth: prometheus.NewDesc(
			"orla_scheduler_queue_depth",
			"Requests currently queued waiting for a worker slot, per backend.",
			[]string{"backend"}, nil,
		),
		inFlight: prometheus.NewDesc(
			"orla_scheduler_in_flight",
			"Requests currently being dispatched, per backend.",
			[]string{"backend"}, nil,
		),
		capacity: prometheus.NewDesc(
			"orla_scheduler_capacity",
			"Configured max_concurrency for the backend.",
			[]string{"backend"}, nil,
		),
		dispatched: prometheus.NewDesc(
			"orla_scheduler_dispatched_total",
			"Cumulative count of dispatches initiated, per backend.",
			[]string{"backend"}, nil,
		),
		circuitState: prometheus.NewDesc(
			"orla_circuit_breaker_state",
			"Circuit breaker state per backend. Exactly one state label is 1, the others are 0.",
			[]string{"backend", "state"}, nil,
		),
	}
}

func (c *SchedulerCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.queueDepth
	ch <- c.inFlight
	ch <- c.capacity
	ch <- c.dispatched
	ch <- c.circuitState
}

// cbStates is the fixed set of circuit breaker state label values.
var cbStates = []string{"closed", "open", "half-open"}

func (c *SchedulerCollector) Collect(ch chan<- prometheus.Metric) {
	for _, s := range c.src.Stats() {
		ch <- prometheus.MustNewConstMetric(c.queueDepth, prometheus.GaugeValue, float64(s.QueueDepth), s.Backend)
		ch <- prometheus.MustNewConstMetric(c.inFlight, prometheus.GaugeValue, float64(s.InFlight), s.Backend)
		ch <- prometheus.MustNewConstMetric(c.capacity, prometheus.GaugeValue, float64(s.Capacity), s.Backend)
		ch <- prometheus.MustNewConstMetric(c.dispatched, prometheus.CounterValue, float64(s.Dispatched), s.Backend)
		for _, state := range cbStates {
			active := 0.0
			if s.CircuitState == state {
				active = 1.0
			}
			ch <- prometheus.MustNewConstMetric(c.circuitState, prometheus.GaugeValue, active, s.Backend, state)
		}
	}
}

// BatchWriterStats is the subset of stats every BatchWriter exposes.
// CompletionWriter and FeedbackWriter both satisfy this, the collector
// uses it to emit drops/flushes/failures per writer kind.
type BatchWriterStats interface {
	Drops() int64
	Flushes() int64
	Failures() int64
}

// BatchWriterCollector emits drops, flushes, and failures per named
// writer.
type BatchWriterCollector struct {
	writers map[string]BatchWriterStats

	drops    *prometheus.Desc
	flushes  *prometheus.Desc
	failures *prometheus.Desc
}

// NewBatchWriterCollector takes a map of writer-kind to stats source.
// Typical usage: NewBatchWriterCollector(map[string]BatchWriterStats{
//     "completion_records": completionWriter,
//     "feedback":           feedbackWriter,
// }).
func NewBatchWriterCollector(writers map[string]BatchWriterStats) *BatchWriterCollector {
	return &BatchWriterCollector{
		writers: writers,
		drops: prometheus.NewDesc(
			"orla_batch_writer_drops_total",
			"Items dropped because the batch writer's buffer was full or closed.",
			[]string{"kind"}, nil,
		),
		flushes: prometheus.NewDesc(
			"orla_batch_writer_flushes_total",
			"Successful flush batches, per writer kind.",
			[]string{"kind"}, nil,
		),
		failures: prometheus.NewDesc(
			"orla_batch_writer_failures_total",
			"Failed flush attempts, per writer kind.",
			[]string{"kind"}, nil,
		),
	}
}

func (c *BatchWriterCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.drops
	ch <- c.flushes
	ch <- c.failures
}

func (c *BatchWriterCollector) Collect(ch chan<- prometheus.Metric) {
	for kind, w := range c.writers {
		ch <- prometheus.MustNewConstMetric(c.drops, prometheus.CounterValue, float64(w.Drops()), kind)
		ch <- prometheus.MustNewConstMetric(c.flushes, prometheus.CounterValue, float64(w.Flushes()), kind)
		ch <- prometheus.MustNewConstMetric(c.failures, prometheus.CounterValue, float64(w.Failures()), kind)
	}
}
