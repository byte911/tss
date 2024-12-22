package scheduler

import (
	"context"

	"github.com/t77yq/nats-project/internal/model"
)

// Scheduler defines the interface for task schedulers
type Scheduler interface {
	// Start starts the scheduler
	Start(ctx context.Context) error

	// Stop stops the scheduler
	Stop()

	// SubmitTask submits a task for execution
	SubmitTask(ctx context.Context, task *model.Task) error

	// CancelTask cancels a running or pending task
	CancelTask(ctx context.Context, taskID string) error

	// GetTaskStatus returns the status of a task
	GetTaskStatus(ctx context.Context, taskID string) (*model.Task, error)

	// ListTasks retrieves a list of tasks based on filters
	ListTasks(ctx context.Context, filters TaskFilters) ([]*model.Task, error)

	// AddSchedule adds a new cron schedule
	AddSchedule(ctx context.Context, schedule *model.CronSchedule) error

	// RemoveSchedule removes a cron schedule
	RemoveSchedule(id string) error

	// GetSchedule gets a cron schedule by ID
	GetSchedule(id string) (*model.CronSchedule, error)

	// ListSchedules lists all cron schedules
	ListSchedules() []*model.CronSchedule
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

// ExecutorManager defines the interface for executor management
type ExecutorManager interface {
	// RegisterExecutor registers a new executor
	RegisterExecutor(executor *model.Executor) error

	// UnregisterExecutor unregisters an executor
	UnregisterExecutor(executorID string)

	// UpdateExecutorStats updates executor statistics
	UpdateExecutorStats(executorID string, stats *model.ExecutorStats) error

	// SelectExecutor selects an executor for a task
	SelectExecutor(task *model.Task) (*model.Executor, error)
}

// Executor represents a task executor node
type Executor struct {
	ID       string            `json:"id"`
	Hostname string            `json:"hostname"`
	Address  string            `json:"address"`
	Tags     map[string]string `json:"tags"`
	Status   string            `json:"status"`
}
