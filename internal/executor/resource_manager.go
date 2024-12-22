package executor

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"go.uber.org/zap"

	"github.com/t77yq/nats-project/internal/model"
)

// ResourceLimits defines resource limits for task execution
type ResourceLimits struct {
	MaxCPU    float64 // Maximum CPU usage in percentage
	MaxMemory int64   // Maximum memory usage in bytes
	MaxTasks  int     // Maximum concurrent tasks
}

// ResourceManager manages system resources and processes
type ResourceManager struct {
	logger    *zap.Logger
	limits    ResourceLimits
	mu        sync.RWMutex
	stats     *model.ExecutorStats
	processes map[string]*exec.Cmd
}

// NewResourceManager creates a new resource manager
func NewResourceManager(limits ResourceLimits, logger *zap.Logger) (*ResourceManager, error) {
	return &ResourceManager{
		logger:    logger.Named("resource-manager"),
		limits:    limits,
		processes: make(map[string]*exec.Cmd),
		stats:     &model.ExecutorStats{
			CollectedAt: time.Now(),
		},
	}, nil
}

// Start starts the resource manager
func (rm *ResourceManager) Start(ctx context.Context) error {
	rm.logger.Info("Starting resource manager")

	// Start resource monitoring
	go rm.monitorResources(ctx)

	return nil
}

// Stop stops the resource manager
func (rm *ResourceManager) Stop() {
	rm.logger.Info("Stopping resource manager")
	
	// Stop all running processes
	rm.mu.Lock()
	defer rm.mu.Unlock()

	for id, cmd := range rm.processes {
		if cmd.Process != nil {
			if err := cmd.Process.Kill(); err != nil {
				rm.logger.Error("Failed to kill process",
					zap.String("process_id", id),
					zap.Error(err))
			}
		}
	}
}

// CreateProcess creates a new process for task execution
func (rm *ResourceManager) CreateProcess(ctx context.Context, task *model.Task) (string, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Check resource limits
	if len(rm.processes) >= rm.limits.MaxTasks {
		return "", fmt.Errorf("maximum number of concurrent tasks reached")
	}

	// Create command
	cmd := exec.CommandContext(ctx, "sh", "-c", task.Command)
	
	// Set working directory if specified
	if task.WorkingDir != "" {
		cmd.Dir = task.WorkingDir
	}

	// Store process info
	processID := task.ID
	rm.processes[processID] = cmd

	rm.logger.Info("Process created",
		zap.String("process_id", processID),
		zap.String("task_id", task.ID))

	return processID, nil
}

// StartProcess starts a process
func (rm *ResourceManager) StartProcess(ctx context.Context, processID string) error {
	rm.mu.Lock()
	cmd, ok := rm.processes[processID]
	rm.mu.Unlock()

	if !ok {
		return fmt.Errorf("process not found: %s", processID)
	}

	// Start process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	rm.logger.Info("Process started", zap.String("process_id", processID))
	return nil
}

// StopProcess stops a process
func (rm *ResourceManager) StopProcess(ctx context.Context, processID string) error {
	rm.mu.Lock()
	cmd, ok := rm.processes[processID]
	rm.mu.Unlock()

	if !ok {
		return fmt.Errorf("process not found: %s", processID)
	}

	// Stop process
	if cmd.Process != nil {
		if err := cmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to stop process: %w", err)
		}
	}

	rm.logger.Info("Process stopped", zap.String("process_id", processID))
	return nil
}

// RemoveProcess removes a process
func (rm *ResourceManager) RemoveProcess(ctx context.Context, processID string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if _, ok := rm.processes[processID]; !ok {
		return fmt.Errorf("process not found: %s", processID)
	}

	delete(rm.processes, processID)
	rm.logger.Info("Process removed", zap.String("process_id", processID))
	return nil
}

// GetStats returns current resource statistics
func (rm *ResourceManager) GetStats() *model.ExecutorStats {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.stats
}

// monitorResources monitors system resource usage
func (rm *ResourceManager) monitorResources(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rm.collectResourceStats()
		}
	}
}

// collectResourceStats collects system resource statistics
func (rm *ResourceManager) collectResourceStats() {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Collect CPU usage
	cpuPercent, err := cpu.Percent(time.Second, false)
	if err != nil {
		rm.logger.Error("Failed to get CPU usage", zap.Error(err))
	} else if len(cpuPercent) > 0 {
		rm.stats.CPUUsage = cpuPercent[0]
	}

	// Collect memory usage
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		rm.logger.Error("Failed to get memory usage", zap.Error(err))
	} else {
		rm.stats.MemoryUsage = memInfo.UsedPercent
	}

	// Update process count
	rm.stats.TaskCount = len(rm.processes)
	rm.stats.CollectedAt = time.Now()

	rm.logger.Debug("Resource stats collected",
		zap.Float64("cpu_usage", rm.stats.CPUUsage),
		zap.Float64("memory_usage", rm.stats.MemoryUsage),
		zap.Int("task_count", rm.stats.TaskCount))
}
