package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"github.com/t77yq/nats-project/internal/model"
)

// TaskHandler defines the interface for task execution
type TaskHandler interface {
	Execute(ctx context.Context, task *model.Task) (*model.TaskResult, error)
}

// Executor represents a task executor node
type Executor struct {
	id       string
	js       nats.JetStreamContext
	logger   *zap.Logger
	handlers map[string]TaskHandler
	tasks    sync.Map // Currently running tasks
}

// NewExecutor creates a new task executor
func NewExecutor(js nats.JetStreamContext, logger *zap.Logger) (*Executor, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("failed to get hostname: %w", err)
	}

	executor := &Executor{
		id:       fmt.Sprintf("%s-%d", hostname, time.Now().UnixNano()),
		js:       js,
		logger:   logger,
		handlers: make(map[string]TaskHandler),
	}

	if err := executor.setup(); err != nil {
		return nil, err
	}

	return executor, nil
}

func (e *Executor) setup() error {
	// Subscribe to task submissions
	_, err := e.js.Subscribe("task.submit", func(msg *nats.Msg) {
		var task model.Task
		if err := json.Unmarshal(msg.Data, &task); err != nil {
			e.logger.Error("Failed to unmarshal task", zap.Error(err))
			return
		}

		// Handle the task asynchronously
		go e.handleTask(&task)
	})

	return err
}

// RegisterHandler registers a task handler for a specific task type
func (e *Executor) RegisterHandler(taskType string, handler TaskHandler) {
	e.handlers[taskType] = handler
}

func (e *Executor) handleTask(task *model.Task) {
	// Check if we can handle this task type
	handler, ok := e.handlers[task.Name]
	if !ok {
		e.logger.Info("No handler registered for task type", zap.String("type", task.Name))
		return
	}

	// Update task status to running
	task.Status = model.TaskStatusRunning
	task.ExecutorID = e.id
	startTime := time.Now()
	task.StartedAt = &startTime

	// Store task in running tasks
	e.tasks.Store(task.ID, task)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// Execute the task
	result, err := handler.Execute(ctx, task)
	if err != nil {
		result = &model.TaskResult{
			TaskID:      task.ID,
			ExecutorID:  e.id,
			Status:      model.TaskStatusFailed,
			Error:       err.Error(),
			CompletedAt: time.Now(),
		}
	}

	// Publish the result
	resultData, err := json.Marshal(result)
	if err != nil {
		e.logger.Error("Failed to marshal task result", zap.Error(err))
		return
	}

	_, err = e.js.Publish("task.result", resultData)
	if err != nil {
		e.logger.Error("Failed to publish task result", zap.Error(err))
	}

	// Remove task from running tasks
	e.tasks.Delete(task.ID)
}

// GetRunningTasks returns a list of currently running tasks
func (e *Executor) GetRunningTasks() []*model.Task {
	var tasks []*model.Task
	e.tasks.Range(func(key, value interface{}) bool {
		tasks = append(tasks, value.(*model.Task))
		return true
	})
	return tasks
}
