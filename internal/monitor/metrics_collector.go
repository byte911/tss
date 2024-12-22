package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"go.uber.org/zap"

	"github.com/t77yq/nats-project/internal/model"
)

// MetricsCollector collects system and task metrics
type MetricsCollector struct {
	logger   *zap.Logger
	js       nats.JetStreamContext
	interval time.Duration
	mu       sync.RWMutex
	metrics  map[string]*model.ExecutorStats
	stop     chan struct{}
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(js nats.JetStreamContext, interval time.Duration, logger *zap.Logger) *MetricsCollector {
	return &MetricsCollector{
		logger:   logger.Named("metrics-collector"),
		js:       js,
		interval: interval,
		metrics:  make(map[string]*model.ExecutorStats),
		stop:     make(chan struct{}),
	}
}

// Start starts the metrics collector
func (c *MetricsCollector) Start(ctx context.Context) error {
	c.logger.Info("Starting metrics collector")

	// Subscribe to executor stats
	if _, err := c.js.Subscribe("executor.stats.*", c.handleExecutorStats); err != nil {
		return fmt.Errorf("failed to subscribe to executor stats: %w", err)
	}

	// Start collection loop
	go c.collectLoop(ctx)

	return nil
}

// Stop stops the metrics collector
func (c *MetricsCollector) Stop() {
	c.logger.Info("Stopping metrics collector")
	close(c.stop)
}

// handleExecutorStats handles executor statistics updates
func (c *MetricsCollector) handleExecutorStats(msg *nats.Msg) {
	var stats model.ExecutorStats
	if err := json.Unmarshal(msg.Data, &stats); err != nil {
		c.logger.Error("Failed to unmarshal executor stats", zap.Error(err))
		return
	}

	// Extract executor ID from the subject
	// Subject format: executor.stats.<executor_id>
	parts := strings.Split(msg.Subject, ".")
	if len(parts) != 3 {
		c.logger.Error("Invalid executor stats subject",
			zap.String("subject", msg.Subject))
		return
	}
	executorID := parts[2]

	c.mu.Lock()
	c.metrics[executorID] = &stats
	c.mu.Unlock()
}

// collectLoop runs the metrics collection loop
func (c *MetricsCollector) collectLoop(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stop:
			return
		case <-ticker.C:
			c.collectMetrics()
		}
	}
}

// collectMetrics collects system metrics
func (c *MetricsCollector) collectMetrics() {
	// Collect CPU usage
	cpuPercent, err := cpu.Percent(time.Second, false)
	if err != nil {
		c.logger.Error("Failed to get CPU usage", zap.Error(err))
		return
	}

	// Collect memory usage
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		c.logger.Error("Failed to get memory usage", zap.Error(err))
		return
	}

	// Create metrics data
	metrics := struct {
		Timestamp   time.Time  `json:"timestamp"`
		CPUUsage    float64   `json:"cpu_usage"`
		MemoryUsage float64   `json:"memory_usage"`
		Executors   []*model.ExecutorStats `json:"executors"`
	}{
		Timestamp:   time.Now(),
		CPUUsage:    cpuPercent[0],
		MemoryUsage: memInfo.UsedPercent,
	}

	// Add executor metrics
	c.mu.RLock()
	for _, stats := range c.metrics {
		metrics.Executors = append(metrics.Executors, stats)
	}
	c.mu.RUnlock()

	// Publish metrics
	data, err := json.Marshal(metrics)
	if err != nil {
		c.logger.Error("Failed to marshal metrics", zap.Error(err))
		return
	}

	if _, err := c.js.Publish("metrics.system", data); err != nil {
		c.logger.Error("Failed to publish metrics", zap.Error(err))
		return
	}

	c.logger.Debug("Metrics collected",
		zap.Float64("cpu_usage", metrics.CPUUsage),
		zap.Float64("memory_usage", metrics.MemoryUsage),
		zap.Int("executor_count", len(metrics.Executors)))
}

// GetMetrics returns the current metrics
func (c *MetricsCollector) GetMetrics() map[string]*model.ExecutorStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	metrics := make(map[string]*model.ExecutorStats)
	for id, stats := range c.metrics {
		metrics[id] = stats
	}
	return metrics
}
