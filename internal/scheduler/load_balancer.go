package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/t77yq/nats-project/internal/model"
)

// BalancingStrategy defines the interface for load balancing strategies
type BalancingStrategy interface {
	SelectExecutor(executors map[string]*model.Executor, task *model.Task) (*model.Executor, error)
}

// RoundRobinStrategy implements round-robin load balancing
type RoundRobinStrategy struct {
	current int
	mu      sync.Mutex
}

// SelectExecutor selects an executor using round-robin strategy
func (s *RoundRobinStrategy) SelectExecutor(executors map[string]*model.Executor, task *model.Task) (*model.Executor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get healthy executors
	var healthy []*model.Executor
	for _, e := range executors {
		if e.Status == model.ExecutorStatusHealthy {
			healthy = append(healthy, e)
		}
	}

	if len(healthy) == 0 {
		return nil, ErrNoHealthyExecutors
	}

	// Select next executor
	executor := healthy[s.current%len(healthy)]
	s.current++

	return executor, nil
}

// LeastLoadStrategy implements least-load balancing
type LeastLoadStrategy struct{}

// SelectExecutor selects an executor with the least load
func (s *LeastLoadStrategy) SelectExecutor(executors map[string]*model.Executor, task *model.Task) (*model.Executor, error) {
	var selected *model.Executor
	minLoad := -1

	for _, e := range executors {
		if e.Status != model.ExecutorStatusHealthy {
			continue
		}

		// Calculate load score (simple version)
		load := e.TaskCount

		if minLoad == -1 || load < minLoad {
			minLoad = load
			selected = e
		}
	}

	if selected == nil {
		return nil, ErrNoHealthyExecutors
	}

	return selected, nil
}

// LoadBalancer implements load balancing across executors
type LoadBalancer struct {
	logger    *zap.Logger
	executors map[string]*model.Executor
	strategy  BalancingStrategy
	mu        sync.RWMutex
	stop      chan struct{}
}

// NewLoadBalancer creates a new load balancer
func NewLoadBalancer(strategy BalancingStrategy, logger *zap.Logger) *LoadBalancer {
	return &LoadBalancer{
		logger:    logger.Named("load-balancer"),
		executors: make(map[string]*model.Executor),
		strategy:  strategy,
		stop:      make(chan struct{}),
	}
}

// Start starts the load balancer
func (lb *LoadBalancer) Start(ctx context.Context) error {
	lb.logger.Info("Starting load balancer")

	// Start health check loop
	go lb.healthCheckLoop(ctx)

	return nil
}

// Stop stops the load balancer
func (lb *LoadBalancer) Stop() {
	lb.logger.Info("Stopping load balancer")
	close(lb.stop)
}

// RegisterExecutor registers a new executor
func (lb *LoadBalancer) RegisterExecutor(executor *model.Executor) error {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	if _, exists := lb.executors[executor.ID]; exists {
		return fmt.Errorf("executor %s already registered", executor.ID)
	}

	executor.Status = model.ExecutorStatusHealthy
	executor.LastHeartbeat = time.Now()
	lb.executors[executor.ID] = executor

	lb.logger.Info("Executor registered",
		zap.String("executor_id", executor.ID))

	return nil
}

// UnregisterExecutor unregisters an executor
func (lb *LoadBalancer) UnregisterExecutor(executorID string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	if executor, exists := lb.executors[executorID]; exists {
		executor.Status = model.ExecutorStatusOffline
		delete(lb.executors, executorID)

		lb.logger.Info("Executor unregistered",
			zap.String("executor_id", executorID))
	}
}

// UpdateExecutorStats updates executor statistics
func (lb *LoadBalancer) UpdateExecutorStats(executorID string, stats *model.ExecutorStats) error {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	executor, exists := lb.executors[executorID]
	if !exists {
		return fmt.Errorf("executor %s not found", executorID)
	}

	executor.TaskCount = stats.TaskCount
	executor.CPU = stats.CPUUsage
	executor.Memory = stats.MemoryUsage
	executor.LastHeartbeat = time.Now()

	return nil
}

// SelectExecutor selects an executor for a task
func (lb *LoadBalancer) SelectExecutor(task *model.Task) (*model.Executor, error) {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	return lb.strategy.SelectExecutor(lb.executors, task)
}

// healthCheckLoop runs periodic health checks on executors
func (lb *LoadBalancer) healthCheckLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-lb.stop:
			return
		case <-ticker.C:
			lb.checkExecutorHealth()
		}
	}
}

// checkExecutorHealth checks the health of all executors
func (lb *LoadBalancer) checkExecutorHealth() {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	now := time.Now()
	timeout := 15 * time.Second

	for id, executor := range lb.executors {
		if now.Sub(executor.LastHeartbeat) > timeout {
			if executor.Status == model.ExecutorStatusHealthy {
				executor.Status = model.ExecutorStatusUnhealthy
				lb.logger.Warn("Executor marked as unhealthy",
					zap.String("executor_id", id),
					zap.Time("last_heartbeat", executor.LastHeartbeat))
			}
		} else if executor.Status == model.ExecutorStatusUnhealthy {
			executor.Status = model.ExecutorStatusHealthy
			lb.logger.Info("Executor marked as healthy",
				zap.String("executor_id", id))
		}
	}
}
