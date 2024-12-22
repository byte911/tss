package model

import "time"

// AlertSeverity represents the severity level of an alert
type AlertSeverity string

const (
	AlertSeverityInfo     AlertSeverity = "info"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityError    AlertSeverity = "error"
	AlertSeverityCritical AlertSeverity = "critical"
)

// AlertType represents the type of alert
type AlertType string

const (
	AlertTypeTimeout       AlertType = "execution_timeout"
	AlertTypeResourceUsage AlertType = "resource_usage"
	AlertTypeTaskFailure   AlertType = "task_failure"
)

// AlertRule defines a rule for generating alerts
type AlertRule struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Type        AlertType     `json:"type"`
	Duration    string        `json:"duration,omitempty"`
	Threshold   float64       `json:"threshold,omitempty"`
	Severity    AlertSeverity `json:"severity"`
	Silenced    bool         `json:"silenced"`
	NotifyEmail string        `json:"notify_email,omitempty"`
	NotifyURL   string        `json:"notify_url,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

// Alert represents an alert event
type Alert struct {
	ID        string                 `json:"id"`
	RuleID    string                 `json:"rule_id"`
	Type      AlertType              `json:"type"`
	Severity  AlertSeverity          `json:"severity"`
	Message   string                 `json:"message"`
	Data      map[string]interface{} `json:"data,omitempty"`
	CreatedAt time.Time             `json:"created_at"`
	ResolvedAt *time.Time           `json:"resolved_at,omitempty"`
}
