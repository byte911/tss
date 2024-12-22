package model

import (
	"time"
)

// TaskStatus represents the status of a task
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusBlocked   TaskStatus = "blocked"
	TaskStatusReady     TaskStatus = "ready"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled"
	TaskStatusSkipped   TaskStatus = "skipped"
	TaskStatusTimeout   TaskStatus = "timeout"
)

// TaskPriority represents the priority of a task
type TaskPriority string

const (
	TaskPriorityHigh   TaskPriority = "high"
	TaskPriorityNormal TaskPriority = "normal"
	TaskPriorityLow    TaskPriority = "low"
	TaskPriorityCritical TaskPriority = "critical"
)

// Task represents a task to be executed
type Task struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Type        string       `json:"type"`
	Priority    TaskPriority `json:"priority"`
	Status      TaskStatus   `json:"status"`
	Payload     []byte       `json:"payload"`
	Result      []byte       `json:"result,omitempty"`
	Error       string       `json:"error,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	ScheduledAt time.Time    `json:"scheduled_at"`
	StartedAt   *time.Time   `json:"started_at,omitempty"`
	CompletedAt *time.Time   `json:"completed_at,omitempty"`
	LastAttempt time.Time    `json:"last_attempt"`
	Attempts    int          `json:"attempts"`

	// Dependency fields
	Dependencies   []string          `json:"dependencies,omitempty"`
	DependencyInfo map[string]string `json:"dependency_info,omitempty"`
	DependentTasks []string          `json:"dependent_tasks,omitempty"`

	// Execution fields
	Command     string  `json:"command,omitempty"`     // Command to execute
	WorkingDir  string  `json:"working_dir,omitempty"` // Working directory for command execution
	ExecutorID  string  `json:"executor_id,omitempty"`
	CPU         float64 `json:"cpu,omitempty"`
	Memory      float64 `json:"memory,omitempty"`
}

// TaskResult represents the result of a task execution
type TaskResult struct {
	TaskID      string     `json:"task_id"`
	Status      TaskStatus `json:"status"`
	Result      []byte     `json:"result,omitempty"`
	Error       string     `json:"error,omitempty"`
	CompletedAt time.Time  `json:"completed_at"`
}
