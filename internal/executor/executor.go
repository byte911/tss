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
}

// NewExecutor creates a new executor
func NewExecutor(js nats.JetStreamContext, logger *zap.Logger, history storage.TaskHistoryStorage) (*Executor, error) {
	executor := &Executor{
		logger:   logger,
		js:       js,
		handlers: make(map[string]TaskHandler),
		history:  history,
	}

	// Setup streams and subjects
	if err := executor.setup(); err != nil {
		return nil, err
	}

	return executor, nil
}

// setup initializes NATS streams and subscriptions
func (e *Executor) setup() error {
	// Create or update the task stream
	streamInfo, err := e.js.StreamInfo(taskStreamName)
	if err != nil && err != nats.ErrStreamNotFound {
		return fmt.Errorf("failed to get stream info: %w", err)
	}

	if streamInfo == nil {
		// Create new stream
		_, err = e.js.AddStream(&nats.StreamConfig{
			Name:       taskStreamName,
			Subjects:   []string{taskStreamSubjects, taskResultSubject},
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
			return fmt.Errorf("failed to create stream: %w", err)
		}
		e.logger.Info("Created task stream", zap.String("name", taskStreamName))
	} else {
		// Update existing stream while preserving retention policy
		config := streamInfo.Config
		config.Subjects = []string{taskStreamSubjects, taskResultSubject}
		config.MaxAge = 24 * time.Hour
		config.MaxMsgSize = 1 * 1024 * 1024
		config.Duplicates = time.Hour

		_, err = e.js.UpdateStream(&config)
		if err != nil {
			return fmt.Errorf("failed to update stream: %w", err)
		}
		e.logger.Info("Updated task stream", zap.String("name", taskStreamName))
	}

	// Subscribe to task submissions with queue group
	_, err = e.js.QueueSubscribe(
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

// publishResult publishes the task result to NATS
func (e *Executor) publishResult(taskID string, result *model.TaskResult) error {
	if result == nil {
		result = &model.TaskResult{
			TaskID:      taskID,
			Status:      model.TaskStatusFailed,
			Error:       "task execution failed with nil result",
			CompletedAt: time.Now(),
		}
	}

	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}

	subject := fmt.Sprintf("task.result.%s", taskID)
	_, err = e.js.Publish(subject, data, nats.ExpectStream(taskStreamName))
	if err != nil {
		return fmt.Errorf("failed to publish result: %w", err)
	}

	e.logger.Debug("Published task result",
		zap.String("task_id", taskID),
		zap.String("subject", subject),
		zap.String("status", string(result.Status)))

	return nil
}

// GetTaskHistory retrieves task execution history
func (e *Executor) GetTaskHistory(ctx context.Context, filters map[string]interface{}, offset, limit int) ([]*storage.TaskHistory, error) {
	return e.history.List(ctx, filters, offset, limit)
}

// GetTaskHistoryByID retrieves a specific task history record
func (e *Executor) GetTaskHistoryByID(ctx context.Context, id string) (*storage.TaskHistory, error) {
	return e.history.Get(ctx, id)
}

// CleanupOldHistory deletes task history records older than the specified time
func (e *Executor) CleanupOldHistory(ctx context.Context, before time.Time) error {
	return e.history.DeleteBefore(ctx, before)
}
