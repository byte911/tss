package scheduler

import (
	"context"

	"github.com/t77yq/nats-project/internal/model"
)

// Scheduler defines the interface for task scheduling operations
type Scheduler interface {
	// SubmitTask submits a new task to the scheduler
	SubmitTask(ctx context.Context, task *model.Task) error

	// CancelTask cancels a running or pending task
	CancelTask(ctx context.Context, taskID string) error

	// GetTaskStatus retrieves the current status of a task
	GetTaskStatus(ctx context.Context, taskID string) (*model.Task, error)

	// ListTasks retrieves a list of tasks based on filters
	ListTasks(ctx context.Context, filters TaskFilters) ([]*model.Task, error)
}

// TaskFilters defines the filters for listing tasks
type TaskFilters struct {
	Status    []model.TaskStatus
	Priority  []model.TaskPriority
	FromTime  string
	ToTime    string
	Limit     int
	Offset    int
}

// ExecutorRegistry manages task executor registration and discovery
type ExecutorRegistry interface {
	// RegisterExecutor registers a new task executor
	RegisterExecutor(ctx context.Context, executor *Executor) error

	// UnregisterExecutor removes a task executor
	UnregisterExecutor(ctx context.Context, executorID string) error

	// ListExecutors returns a list of available executors
	ListExecutors(ctx context.Context) ([]*Executor, error)
}

// Executor represents a task executor node
type Executor struct {
	ID       string            `json:"id"`
	Hostname string            `json:"hostname"`
	Address  string            `json:"address"`
	Tags     map[string]string `json:"tags"`
	Status   string            `json:"status"`
}
