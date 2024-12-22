package model

import (
	"encoding/json"
	"time"
)

// TaskStatus represents the status of a task
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusBlocked   TaskStatus = "blocked"    // New status for tasks blocked by dependencies
	TaskStatusReady     TaskStatus = "ready"      // New status for tasks with satisfied dependencies
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusComplete  TaskStatus = "complete"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled"
)

// TaskPriority represents the priority of a task
type TaskPriority string

const (
	TaskPriorityLow    TaskPriority = "low"
	TaskPriorityMedium TaskPriority = "medium"
	TaskPriorityHigh   TaskPriority = "high"
)

// Task represents a unit of work to be executed
type Task struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Priority    TaskPriority    `json:"priority"`
	Status      TaskStatus      `json:"status"`
	Payload     json.RawMessage `json:"payload"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
	
	// Task dependencies
	Dependencies    []string          `json:"dependencies,omitempty"`    // IDs of tasks that must complete before this task can start
	DependencyInfo  map[string]string `json:"dependency_info,omitempty"` // Additional information about dependencies (e.g., why a task depends on another)
	DependentTasks  []string          `json:"dependent_tasks,omitempty"` // IDs of tasks that depend on this task
	
	// Timing information
	CreatedAt     time.Time  `json:"created_at"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	NextRetryTime *time.Time `json:"next_retry_time,omitempty"`
	
	// Retry configuration
	MaxRetries    int `json:"max_retries"`
	RetryCount    int `json:"retry_count"`
	RetryInterval int `json:"retry_interval"` // in seconds
}

// TaskResult represents the result of a task execution
type TaskResult struct {
	TaskID      string          `json:"task_id"`
	Status      TaskStatus      `json:"status"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
	CompletedAt time.Time       `json:"completed_at"`
}
