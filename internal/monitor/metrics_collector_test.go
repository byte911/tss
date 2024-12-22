package monitor

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/t77yq/nats-project/internal/model"
	"github.com/t77yq/nats-project/internal/testutil"
)

func TestMetricsCollector(t *testing.T) {
	// Start NATS server with JetStream
	_, js, cleanup := testutil.StartJetStream(t)
	defer cleanup()

	// Create metrics collector
	logger := zaptest.NewLogger(t)
	collector := NewMetricsCollector(js, 1*time.Second, logger)

	// Create stream for metrics
	_, err := js.AddStream(&nats.StreamConfig{
		Name:     "METRICS",
		Subjects: []string{"metrics.*", "executor.stats.*"},
		Storage:  nats.FileStorage,
	})
	require.NoError(t, err)

	// Start collector
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)

	t.Run("CollectMetrics", func(t *testing.T) {
		// Wait for metrics collection
		time.Sleep(2 * time.Second)

		// Verify metrics were published
		msgs, err := testutil.ConsumeMessages(js, "metrics.system", time.Second)
		require.NoError(t, err)
		assert.NotEmpty(t, msgs)

		// Verify metrics format
		var metrics struct {
			Timestamp   time.Time            `json:"timestamp"`
			CPUUsage    float64             `json:"cpu_usage"`
			MemoryUsage float64             `json:"memory_usage"`
			Executors   []*model.ExecutorStats `json:"executors"`
		}
		err = json.Unmarshal(msgs[0], &metrics)
		require.NoError(t, err)

		assert.NotZero(t, metrics.Timestamp)
		assert.GreaterOrEqual(t, metrics.CPUUsage, 0.0)
		assert.GreaterOrEqual(t, metrics.MemoryUsage, 0.0)
	})

	t.Run("HandleExecutorStats", func(t *testing.T) {
		// Create executor stats
		stats := &model.ExecutorStats{
			TaskCount:   5,
			CPUUsage:    50.0,
			MemoryUsage: 1024 * 1024 * 1024,
			DiskUsage:   80.0,
			NetworkIn:   1000000,
			NetworkOut:  500000,
			CollectedAt: time.Now(),
		}

		// Publish stats
		data, err := json.Marshal(stats)
		require.NoError(t, err)
		_, err = js.Publish("executor.stats.test-executor", data)
		require.NoError(t, err)

		// Wait for stats processing
		time.Sleep(time.Second)

		// Verify stats were stored
		metrics := collector.GetMetrics()
		assert.Contains(t, metrics, "test-executor")
		assert.Equal(t, stats, metrics["test-executor"])
	})
}

func TestMetricsCollectorIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	// Start NATS server with JetStream
	_, js, cleanup := testutil.StartJetStream(t)
	defer cleanup()

	// Create metrics collector
	logger := zaptest.NewLogger(t)
	collector := NewMetricsCollector(js, 500*time.Millisecond, logger)

	// Create stream for metrics
	_, err := js.AddStream(&nats.StreamConfig{
		Name:     "METRICS",
		Subjects: []string{"metrics.*", "executor.stats.*"},
		Storage:  nats.FileStorage,
	})
	require.NoError(t, err)

	// Start collector
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)

	// Test scenarios
	tests := []struct {
		name       string
		executors  map[string]*model.ExecutorStats
		wantCount  int
		wantMetric bool
	}{
		{
			name: "Multiple Executors",
			executors: map[string]*model.ExecutorStats{
				"exec-1": {
					TaskCount:   3,
					CPUUsage:    30.0,
					MemoryUsage: 512 * 1024 * 1024,
					CollectedAt: time.Now(),
				},
				"exec-2": {
					TaskCount:   5,
					CPUUsage:    50.0,
					MemoryUsage: 1024 * 1024 * 1024,
					CollectedAt: time.Now(),
				},
			},
			wantCount:  2,
			wantMetric: true,
		},
		{
			name:       "No Executors",
			executors:  map[string]*model.ExecutorStats{},
			wantCount:  0,
			wantMetric: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Publish executor stats
			for id, stats := range tt.executors {
				data, err := json.Marshal(stats)
				require.NoError(t, err)
				_, err = js.Publish("executor.stats."+id, data)
				require.NoError(t, err)
			}

			// Wait for stats processing
			time.Sleep(time.Second)

			// Verify executor stats
			metrics := collector.GetMetrics()
			assert.Equal(t, tt.wantCount, len(metrics))
			for id, stats := range tt.executors {
				assert.Equal(t, stats, metrics[id])
			}

			// Verify system metrics
			if tt.wantMetric {
				msgs, err := testutil.ConsumeMessages(js, "metrics.system", time.Second)
				require.NoError(t, err)
				assert.NotEmpty(t, msgs)
			}
		})
	}
}
