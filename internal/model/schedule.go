package model

import (
	"encoding/json"
	"time"
)

// CronSchedule represents a scheduled task
type CronSchedule struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Expression  string       `json:"expression"`
	Payload     json.RawMessage  `json:"payload"`
	Status      TaskStatus   `json:"status"`
	LastRunTime *time.Time   `json:"last_run_time,omitempty"`
	NextRunTime *time.Time   `json:"next_run_time,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}
