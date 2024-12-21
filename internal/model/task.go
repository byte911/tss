package model

import (
	"time"
)

// TaskStatus represents the current status of a task
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCanceled  TaskStatus = "canceled"
)

// TaskPriority represents the priority level of a task
type TaskPriority int

const (
	TaskPriorityLow    TaskPriority = 1
	TaskPriorityNormal TaskPriority = 2
	TaskPriorityHigh   TaskPriority = 3
)

// Task represents a unit of work to be executed
type Task struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Status      TaskStatus   `json:"status"`
	Priority    TaskPriority `json:"priority"`
	Payload     []byte       `json:"payload"`
	RetryCount  int         `json:"retry_count"`
	MaxRetries  int         `json:"max_retries"`
	
	// Timing fields
	CreatedAt     time.Time  `json:"created_at"`
	ScheduledAt   time.Time  `json:"scheduled_at"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	
	// Execution details
	ExecutorID    string     `json:"executor_id,omitempty"`
	ErrorMessage  string     `json:"error_message,omitempty"`
	Result        []byte     `json:"result,omitempty"`
}

// TaskResult represents the result of a task execution
type TaskResult struct {
	TaskID      string    `json:"task_id"`
	ExecutorID  string    `json:"executor_id"`
	Status      TaskStatus `json:"status"`
	Result      []byte    `json:"result,omitempty"`
	Error       string    `json:"error,omitempty"`
	CompletedAt time.Time `json:"completed_at"`
}
