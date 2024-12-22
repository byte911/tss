package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"github.com/t77yq/nats-project/internal/model"
)

// RetryStrategy defines the interface for retry strategies
type RetryStrategy interface {
	// NextRetry calculates the next retry time
	NextRetry(attempt int) time.Duration
}

// ExponentialBackoff implements exponential backoff retry strategy
type ExponentialBackoff struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
}

// NextRetry calculates the next retry time using exponential backoff
func (s *ExponentialBackoff) NextRetry(attempt int) time.Duration {
	delay := float64(s.InitialDelay)
	for i := 0; i < attempt; i++ {
		delay *= s.Multiplier
	}

	if delay > float64(s.MaxDelay) {
		return s.MaxDelay
	}
	return time.Duration(delay)
}

// RetryManager manages task retries and dead letter handling
type RetryManager struct {
	logger         *zap.Logger
	js             nats.JetStreamContext
	scheduler      Scheduler
	strategy       RetryStrategy
	maxAttempts    int
	retryInterval  time.Duration
	mu             sync.RWMutex
	retryTasks     map[string]*model.Task
	stop           chan struct{}
}

// NewRetryManager creates a new retry manager
func NewRetryManager(js nats.JetStreamContext, scheduler Scheduler, strategy RetryStrategy, maxAttempts int, logger *zap.Logger) *RetryManager {
	return &RetryManager{
		logger:      logger.Named("retry-manager"),
		js:          js,
		scheduler:   scheduler,
		strategy:    strategy,
		maxAttempts: maxAttempts,
		retryTasks:  make(map[string]*model.Task),
		stop:        make(chan struct{}),
	}
}

// Start starts the retry manager
func (rm *RetryManager) Start(ctx context.Context) error {
	rm.logger.Info("Starting retry manager")

	// Create dead letter queue stream
	if _, err := rm.js.AddStream(&nats.StreamConfig{
		Name:     "DEADLETTER",
		Subjects: []string{"task.deadletter"},
		Storage:  nats.FileStorage,
	}); err != nil {
		return fmt.Errorf("failed to create dead letter stream: %w", err)
	}

	// Subscribe to task results
	if _, err := rm.js.Subscribe("task.result", rm.handleTaskResult); err != nil {
		return fmt.Errorf("failed to subscribe to task results: %w", err)
	}

	// Start retry loop
	go rm.retryLoop(ctx)

	return nil
}

// Stop stops the retry manager
func (rm *RetryManager) Stop() {
	rm.logger.Info("Stopping retry manager")
	close(rm.stop)
}

// AddRetryTask adds a task for retry
func (rm *RetryManager) AddRetryTask(task *model.Task) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	rm.retryTasks[task.ID] = task
	rm.logger.Info("Task added for retry",
		zap.String("task_id", task.ID),
		zap.Int("attempt", task.Attempts))
}

// handleTaskResult handles task execution results
func (rm *RetryManager) handleTaskResult(msg *nats.Msg) {
	var result model.TaskResult
	if err := json.Unmarshal(msg.Data, &result); err != nil {
		rm.logger.Error("Failed to unmarshal task result",
			zap.Error(err))
		return
	}

	// Only handle failed tasks
	if result.Status != model.TaskStatusFailed {
		return
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	task, ok := rm.retryTasks[result.TaskID]
	if !ok {
		// Task not found in retry map, create new retry task
		task = &model.Task{
			ID:       result.TaskID,
			Attempts: 1,
		}
	}

	// Check if max attempts reached
	if task.Attempts >= rm.maxAttempts {
		rm.moveToDeadLetter(task, result.Error)
		delete(rm.retryTasks, task.ID)
		return
	}

	// Schedule retry
	task.Attempts++
	task.Status = model.TaskStatusPending
	rm.retryTasks[task.ID] = task

	rm.logger.Info("Task scheduled for retry",
		zap.String("task_id", task.ID),
		zap.Int("attempt", task.Attempts))
}

// moveToDeadLetter moves a failed task to the dead letter queue
func (rm *RetryManager) moveToDeadLetter(task *model.Task, errorMsg string) {
	deadLetter := struct {
		Task  *model.Task `json:"task"`
		Error string      `json:"error"`
	}{
		Task:  task,
		Error: errorMsg,
	}

	data, err := json.Marshal(deadLetter)
	if err != nil {
		rm.logger.Error("Failed to marshal dead letter",
			zap.Error(err))
		return
	}

	if _, err := rm.js.Publish("task.deadletter", data); err != nil {
		rm.logger.Error("Failed to publish to dead letter queue",
			zap.Error(err))
		return
	}

	rm.logger.Info("Task moved to dead letter queue",
		zap.String("task_id", task.ID),
		zap.Int("attempts", task.Attempts))
}

// retryLoop runs the retry scheduling loop
func (rm *RetryManager) retryLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-rm.stop:
			return
		case <-ticker.C:
			rm.processRetries(ctx)
		}
	}
}

// processRetries processes tasks in the retry queue
func (rm *RetryManager) processRetries(ctx context.Context) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	now := time.Now()

	for id, task := range rm.retryTasks {
		// Calculate next retry time
		nextRetry := task.LastAttempt.Add(rm.strategy.NextRetry(task.Attempts))

		if now.After(nextRetry) {
			// Submit task for retry
			if err := rm.scheduler.SubmitTask(ctx, task); err != nil {
				rm.logger.Error("Failed to submit retry task",
					zap.String("task_id", id),
					zap.Error(err))
				continue
			}

			task.LastAttempt = now
			rm.logger.Info("Task retry submitted",
				zap.String("task_id", id),
				zap.Int("attempt", task.Attempts))
		}
	}
}
