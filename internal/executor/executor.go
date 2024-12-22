package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"github.com/t77yq/nats-project/internal/model"
	"github.com/t77yq/nats-project/internal/storage"
)

const (
	taskStreamName     = "TASKS"
	taskStreamSubjects = "task.*"
	taskResultSubject  = "task.result.*"
)

// ExecutorConfig defines configuration for the executor
type ExecutorConfig struct {
	ID          string
	Name        string
	Tags        []string
	MaxTasks    int
	MaxCPU     float64
	MaxMemory  int64
	LogDir     string
	MaxLogSize int64
	MaxLogAge  time.Duration
}

// TaskHandler defines the interface for task handlers
type TaskHandler interface {
	Execute(ctx context.Context, task *model.Task) (*model.TaskResult, error)
}

// Executor manages task execution
type Executor struct {
	logger       *zap.Logger
	js           nats.JetStreamContext
	handlers     map[string]TaskHandler
	runningTasks sync.Map
	history      storage.TaskHistoryStorage
	// New fields for resource and log management
	config    ExecutorConfig
	resources *ResourceManager
	logs      *LogManager
	stats     *model.ExecutorStats
}

// NewExecutor creates a new executor
func NewExecutor(js nats.JetStreamContext, config ExecutorConfig, logger *zap.Logger, history storage.TaskHistoryStorage) (*Executor, error) {
	// Create resource manager
	resources, err := NewResourceManager(ResourceLimits{
		MaxCPU:    config.MaxCPU,
		MaxMemory: config.MaxMemory,
		MaxTasks:  config.MaxTasks,
	}, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource manager: %w", err)
	}

	// Create log manager
	logs, err := NewLogManager(LogConfig{
		LogDir:        config.LogDir,
		MaxFileSize:   config.MaxLogSize,
		MaxAge:        config.MaxLogAge,
		FlushInterval: 5 * time.Second,
	}, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create log manager: %w", err)
	}

	executor := &Executor{
		logger:    logger,
		js:        js,
		handlers:  make(map[string]TaskHandler),
		history:   history,
		config:    config,
		resources: resources,
		logs:      logs,
		stats:     &model.ExecutorStats{},
	}

	// Setup streams and subjects
	if err := executor.setup(); err != nil {
		return nil, err
	}

	// Start resource and log managers
	if err := resources.Start(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to start resource manager: %w", err)
	}

	if err := logs.Start(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to start log manager: %w", err)
	}

	// Start heartbeat
	go executor.heartbeat()

	return executor, nil
}

// setup initializes NATS streams and subscriptions
func (e *Executor) setup() error {
	// Create or update streams
	streams := []struct {
		name     string
		subjects []string
	}{
		{
			name:     taskStreamName,
			subjects: []string{taskStreamSubjects, taskResultSubject},
		},
		{
			name:     "HEARTBEATS",
			subjects: []string{"executor.heartbeat", "executor.stats.*"},
		},
	}

	for _, stream := range streams {
		// Check if stream exists
		streamInfo, err := e.js.StreamInfo(stream.name)
		if err != nil && err != nats.ErrStreamNotFound {
			return fmt.Errorf("failed to get stream info: %w", err)
		}

		if streamInfo == nil {
			// Create new stream
			_, err = e.js.AddStream(&nats.StreamConfig{
				Name:       stream.name,
				Subjects:   stream.subjects,
				Retention:  nats.LimitsPolicy,
				MaxAge:     24 * time.Hour,
				MaxMsgs:    -1,
				MaxBytes:   -1,
				Discard:    nats.DiscardOld,
				MaxMsgSize: 1 * 1024 * 1024, // 1MB
				Storage:    nats.FileStorage,
				Replicas:   1,
				Duplicates: time.Hour,
			})
			if err != nil {
				return fmt.Errorf("failed to create stream %s: %w", stream.name, err)
			}
			e.logger.Info("Created stream", zap.String("name", stream.name))
		} else {
			// Update existing stream while preserving retention policy
			config := streamInfo.Config
			config.Subjects = stream.subjects
			config.MaxAge = 24 * time.Hour
			config.MaxMsgSize = 1 * 1024 * 1024
			config.Duplicates = time.Hour

			_, err = e.js.UpdateStream(&config)
			if err != nil {
				return fmt.Errorf("failed to update stream %s: %w", stream.name, err)
			}
			e.logger.Info("Updated stream", zap.String("name", stream.name))
		}
	}

	// Subscribe to task submissions with queue group
	_, err := e.js.QueueSubscribe(
		"task.submit",
		"task_executors",
		func(msg *nats.Msg) {
			var task model.Task
			if err := json.Unmarshal(msg.Data, &task); err != nil {
				e.logger.Error("Failed to unmarshal task", zap.Error(err))
				return
			}

			// Handle the task asynchronously
			go func() {
				if err := e.ExecuteTask(context.Background(), &task); err != nil {
					e.logger.Error("Failed to execute task",
						zap.String("task_id", task.ID),
						zap.Error(err))
				}
			}()

			// Acknowledge the message
			if err := msg.Ack(); err != nil {
				e.logger.Error("Failed to acknowledge message", zap.Error(err))
			}
		},
		nats.ManualAck(),
		nats.AckWait(30*time.Second),
		nats.MaxDeliver(3),
	)

	if err != nil {
		return fmt.Errorf("failed to subscribe to tasks: %w", err)
	}

	return nil
}

// RegisterHandler registers a task handler
func (e *Executor) RegisterHandler(name string, handler TaskHandler) {
	e.handlers[name] = handler
}

// ExecuteTask executes a task
func (e *Executor) ExecuteTask(ctx context.Context, task *model.Task) error {
	handler, ok := e.handlers[task.Name]
	if !ok {
		return fmt.Errorf("unknown task type: %s", task.Name)
	}

	// Create process for task
	processID, err := e.resources.CreateProcess(ctx, task)
	if err != nil {
		return fmt.Errorf("failed to create process: %w", err)
	}
	defer e.resources.RemoveProcess(ctx, processID)

	// Create task history record
	historyID := uuid.New().String()
	startTime := time.Now()

	history := &storage.TaskHistory{
		ID:        historyID,
		TaskID:    task.ID,
		Name:      task.Name,
		Status:    model.TaskStatusRunning,
		Payload:   task.Payload,
		StartedAt: startTime,
	}

	if err := e.history.Store(ctx, history); err != nil {
		e.logger.Error("Failed to store task history",
			zap.String("task_id", task.ID),
			zap.Error(err))
	}

	// Track running task
	e.runningTasks.Store(task.ID, task)
	defer e.runningTasks.Delete(task.ID)

	// Start process
	if err := e.resources.StartProcess(ctx, processID); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	// Execute task
	result, err := handler.Execute(ctx, task)
	endTime := time.Now()
	duration := endTime.Sub(startTime)

	// Update task history
	history.CompletedAt = &endTime
	history.Duration = duration

	if err != nil {
		history.Status = model.TaskStatusFailed
		history.Error = err.Error()
	} else {
		history.Status = result.Status
		history.Result = result.Result
		if result.Error != "" {
			history.Error = result.Error
		}
	}

	if err := e.history.Update(ctx, history); err != nil {
		e.logger.Error("Failed to update task history",
			zap.String("task_id", task.ID),
			zap.Error(err))
	}

	// Stop process
	if err := e.resources.StopProcess(ctx, processID); err != nil {
		e.logger.Error("Failed to stop process",
			zap.String("process_id", processID),
			zap.Error(err))
	}

	// Publish result
	if err := e.publishResult(task.ID, result); err != nil {
		e.logger.Error("Failed to publish task result",
			zap.String("task_id", task.ID),
			zap.Error(err))
		return err
	}

	return nil
}

// GetRunningTasks returns a list of currently running tasks
func (e *Executor) GetRunningTasks() []*model.Task {
	var tasks []*model.Task
	e.runningTasks.Range(func(key, value interface{}) bool {
		if task, ok := value.(*model.Task); ok {
			tasks = append(tasks, task)
		}
		return true
	})
	return tasks
}

// GetTaskHistory retrieves task execution history
func (e *Executor) GetTaskHistory(ctx context.Context, filters map[string]interface{}, offset, limit int) ([]*storage.TaskHistory, error) {
	return e.history.List(ctx, filters, offset, limit)
}

// GetTaskHistoryByID retrieves a specific task history record
func (e *Executor) GetTaskHistoryByID(ctx context.Context, id string) (*storage.TaskHistory, error) {
	return e.history.Get(ctx, id)
}

// GetTaskLogs retrieves logs for a task
func (e *Executor) GetTaskLogs(taskID string, start, end time.Time) ([]LogEntry, error) {
	return e.logs.GetLogs(taskID, start, end)
}

// GetStats returns current executor statistics
func (e *Executor) GetStats() *model.ExecutorStats {
	return e.resources.GetStats()
}

// heartbeat sends periodic heartbeat to scheduler
func (e *Executor) heartbeat() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		stats := e.resources.GetStats()
		heartbeat := struct {
			ExecutorID string              `json:"executor_id"`
			Timestamp  time.Time           `json:"timestamp"`
			Stats     *model.ExecutorStats `json:"stats"`
		}{
			ExecutorID: e.config.ID,
			Timestamp:  time.Now(),
			Stats:     stats,
		}

		data, err := json.Marshal(heartbeat)
		if err != nil {
			e.logger.Error("Failed to marshal heartbeat", zap.Error(err))
			continue
		}

		if _, err := e.js.Publish("executor.heartbeat", data); err != nil {
			e.logger.Error("Failed to publish heartbeat", zap.Error(err))
		}
	}
}

// Stop stops the executor
func (e *Executor) Stop() {
	e.logger.Info("Stopping executor")
	e.resources.Stop()
	e.logs.Stop()
}

// publishResult publishes the task result to NATS
func (e *Executor) publishResult(taskID string, result *model.TaskResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}

	_, err = e.js.Publish(fmt.Sprintf("task.result.%s", taskID), data)
	return err
}

// CleanupOldHistory deletes task history records older than the specified time
func (e *Executor) CleanupOldHistory(ctx context.Context, before time.Time) error {
	return e.history.DeleteBefore(ctx, before)
}
