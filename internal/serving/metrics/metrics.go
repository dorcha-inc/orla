// Package metrics provides Prometheus metrics for the Orla serving layer.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

const namespace = "orla"

var (
	// RequestsTotal counts inference requests by backend and status.
	RequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "requests_total",
			Help:      "Total number of inference requests by backend and status",
		},
		[]string{"backend", "status"}, // status: success, error
	)

	// QueueWaitSeconds is the time requests spend waiting in the scheduler queue.
	QueueWaitSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "queue_wait_seconds",
			Help:      "Time spent waiting in the scheduler queue before dispatch",
			Buckets:   prometheus.ExponentialBuckets(0.001, 2, 14), // 1ms to ~8s
		},
		[]string{"backend"},
	)

	// BackendLatencySeconds is the time spent in the LLM backend.
	BackendLatencySeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "backend_latency_seconds",
			Help:      "Time spent in the LLM backend (inference)",
			Buckets:   prometheus.ExponentialBuckets(0.1, 2, 12), // 100ms to ~3min
		},
		[]string{"backend"},
	)

	// QueueDepth is the current number of requests waiting per backend.
	QueueDepth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_depth",
			Help:      "Number of requests currently waiting in the scheduler queue per backend",
		},
		[]string{"backend"},
	)

	// EstimatedCostTotal is the cumulative estimated USD spend per backend.
	EstimatedCostTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "estimated_cost_usd_total",
			Help:      "Cumulative estimated cost in USD per backend",
		},
		[]string{"backend"},
	)

	// EstimatedCostPerRequest is the per-request estimated cost distribution.
	EstimatedCostPerRequest = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "estimated_cost_usd",
			Help:      "Per-request estimated cost in USD",
			Buckets:   []float64{0.0001, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10},
		},
		[]string{"backend"},
	)
)

func init() {
	prometheus.MustRegister(
		RequestsTotal, QueueWaitSeconds, BackendLatencySeconds, QueueDepth,
		EstimatedCostTotal, EstimatedCostPerRequest,
	)
}
