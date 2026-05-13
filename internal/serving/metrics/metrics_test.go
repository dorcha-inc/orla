package metrics

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPrometheusMetricsInitialized tests that the Prometheus metrics are initialized.
// This test is more of a sanity check than a real test, really.
func TestPrometheusMetricsInitialized(t *testing.T) {
	require.NotNil(t, RequestsTotal, "RequestsTotal should be initialized")
	require.NotNil(t, QueueWaitSeconds, "QueueWaitSeconds should be initialized")
	require.NotNil(t, BackendLatencySeconds, "BackendLatencySeconds should be initialized")
	require.NotNil(t, QueueDepth, "QueueDepth should be initialized")
	require.NotNil(t, EstimatedCostTotal, "EstimatedCostTotal should be initialized")
	require.NotNil(t, EstimatedCostPerRequest, "EstimatedCostPerRequest should be initialized")
}
