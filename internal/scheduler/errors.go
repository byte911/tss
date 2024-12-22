package scheduler

import "errors"

var (
	// ErrTaskNotFound is returned when a task is not found
	ErrTaskNotFound = errors.New("task not found")

	// ErrInvalidPriority is returned when an invalid priority is specified
	ErrInvalidPriority = errors.New("invalid task priority")

	// ErrNoHealthyExecutors is returned when no healthy executors are available
	ErrNoHealthyExecutors = errors.New("no healthy executors available")

	// ErrExecutorNotFound is returned when an executor is not found
	ErrExecutorNotFound = errors.New("executor not found")

	// ErrCircularDependency is returned when a circular dependency is detected
	ErrCircularDependency = errors.New("circular dependency detected")

	// ErrMaxRetriesExceeded is returned when max retries are exceeded
	ErrMaxRetriesExceeded = errors.New("maximum retries exceeded")

	// ErrTaskCancelled is returned when a task is cancelled
	ErrTaskCancelled = errors.New("task cancelled")

	// ErrDuplicateTask is returned when a duplicate task is submitted
	ErrDuplicateTask = errors.New("duplicate task")
)
