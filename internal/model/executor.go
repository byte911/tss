package model

import "time"

// ExecutorStatus represents the status of an executor
type ExecutorStatus string

const (
	ExecutorStatusHealthy   ExecutorStatus = "healthy"
	ExecutorStatusUnhealthy ExecutorStatus = "unhealthy"
	ExecutorStatusOffline   ExecutorStatus = "offline"
)

// Executor represents a task executor node
type Executor struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Status        ExecutorStatus `json:"status"`
	LastHeartbeat time.Time      `json:"last_heartbeat"`
	TaskCount     int           `json:"task_count"`
	CPU          float64        `json:"cpu"`
	Memory       float64        `json:"memory"`
	Tags         []string       `json:"tags,omitempty"`
}

// ExecutorStats represents executor performance statistics
type ExecutorStats struct {
	TaskCount     int     `json:"task_count"`
	CPUUsage      float64 `json:"cpu_usage"`
	MemoryUsage   float64 `json:"memory_usage"`
	DiskUsage     float64 `json:"disk_usage"`
	NetworkIn     int64   `json:"network_in"`
	NetworkOut    int64   `json:"network_out"`
	CollectedAt   time.Time `json:"collected_at"`
}
