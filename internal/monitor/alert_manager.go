package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"github.com/t77yq/nats-project/internal/model"
)

// AlertLevel represents the severity level of an alert
type AlertLevel string

const (
	AlertLevelInfo     AlertLevel = "info"
	AlertLevelWarning  AlertLevel = "warning"
	AlertLevelError    AlertLevel = "error"
	AlertLevelCritical AlertLevel = "critical"
)

// AlertRule defines an alert rule
type AlertRule struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Level       AlertLevel  `json:"level"`
	Type        string      `json:"type"` // task_failure, resource_usage, execution_timeout
	Condition   string      `json:"condition"`
	Threshold   float64     `json:"threshold"`
	Duration    string      `json:"duration,omitempty"`
	Silenced    bool        `json:"silenced"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
	Labels      []string    `json:"labels,omitempty"`
	Channels    []string    `json:"channels"` // email, webhook, message
}

// Alert represents an alert instance
type Alert struct {
	ID        string      `json:"id"`
	RuleID    string      `json:"rule_id"`
	Level     AlertLevel  `json:"level"`
	Message   string      `json:"message"`
	Details   interface{} `json:"details,omitempty"`
	Status    string      `json:"status"` // firing, resolved
	StartedAt time.Time   `json:"started_at"`
	EndedAt   *time.Time  `json:"ended_at,omitempty"`
}

// NotificationChannel defines the interface for alert notification channels
type NotificationChannel interface {
	Send(alert *Alert) error
}

// EmailChannel implements email notifications
type EmailChannel struct {
	// Email configuration
}

// WebhookChannel implements webhook notifications
type WebhookChannel struct {
	URL     string
	Headers map[string]string
}

// MessageChannel implements message notifications (e.g., Slack, Teams)
type MessageChannel struct {
	// Message configuration
}

// AlertManager manages alert rules and notifications
type AlertManager struct {
	logger     *zap.Logger
	js         nats.JetStreamContext
	rules      sync.Map
	alerts     sync.Map
	channels   map[string]NotificationChannel
	stop       chan struct{}
}

// NewAlertManager creates a new alert manager
func NewAlertManager(js nats.JetStreamContext, logger *zap.Logger) *AlertManager {
	return &AlertManager{
		logger:   logger.Named("alert-manager"),
		js:       js,
		channels: make(map[string]NotificationChannel),
		stop:     make(chan struct{}),
	}
}

// Start starts the alert manager
func (m *AlertManager) Start(ctx context.Context) error {
	m.logger.Info("Starting alert manager")

	// Create necessary streams
	streams := []struct {
		name     string
		subjects []string
	}{
		{
			name:     "ALERTS",
			subjects: []string{"alert.*"},
		},
		{
			name:     "TASKS",
			subjects: []string{"task.*", "task.*.>"},
		},
		{
			name:     "HEARTBEATS",
			subjects: []string{"heartbeat.*"},
		},
	}

	for _, stream := range streams {
		// Check if stream exists
		_, err := m.js.StreamInfo(stream.name)
		if err != nil {
			if err != nats.ErrStreamNotFound {
				return fmt.Errorf("failed to check stream %s: %w", stream.name, err)
			}
			// Create stream if not exists
			_, err = m.js.AddStream(&nats.StreamConfig{
				Name:     stream.name,
				Subjects: stream.subjects,
				Storage:  nats.FileStorage,
				MaxAge:   24 * time.Hour,
			})
			if err != nil {
				return fmt.Errorf("failed to create stream %s: %w", stream.name, err)
			}
			m.logger.Info("Created stream", zap.String("name", stream.name))
		}
	}

	// Start evaluation loop
	go m.evaluationLoop(ctx)

	return nil
}

// Stop stops the alert manager
func (m *AlertManager) Stop() {
	m.logger.Info("Stopping alert manager")
	close(m.stop)
}

// AddRule adds a new alert rule
func (m *AlertManager) AddRule(rule *AlertRule) error {
	if rule.CreatedAt.IsZero() {
		rule.CreatedAt = time.Now()
	}
	rule.UpdatedAt = time.Now()

	m.rules.Store(rule.ID, rule)
	m.logger.Info("Alert rule added",
		zap.String("id", rule.ID),
		zap.String("name", rule.Name))

	return nil
}

// RemoveRule removes an alert rule
func (m *AlertManager) RemoveRule(id string) {
	m.rules.Delete(id)
	m.logger.Info("Alert rule removed", zap.String("id", id))
}

// GetRule gets an alert rule by ID
func (m *AlertManager) GetRule(id string) (*AlertRule, error) {
	if rule, ok := m.rules.Load(id); ok {
		return rule.(*AlertRule), nil
	}
	return nil, fmt.Errorf("alert rule not found: %s", id)
}

// ListRules lists all alert rules
func (m *AlertManager) ListRules() []*AlertRule {
	var rules []*AlertRule
	m.rules.Range(func(key, value interface{}) bool {
		rules = append(rules, value.(*AlertRule))
		return true
	})
	return rules
}

// GetActiveAlerts gets all active alerts
func (m *AlertManager) GetActiveAlerts() []*Alert {
	var alerts []*Alert
	m.alerts.Range(func(key, value interface{}) bool {
		alert := value.(*Alert)
		if alert.Status == "firing" {
			alerts = append(alerts, alert)
		}
		return true
	})
	return alerts
}

// SilenceRule silences an alert rule
func (m *AlertManager) SilenceRule(id string) error {
	if rule, ok := m.rules.Load(id); ok {
		r := rule.(*AlertRule)
		r.Silenced = true
		r.UpdatedAt = time.Now()
		m.rules.Store(id, r)
		return nil
	}
	return fmt.Errorf("alert rule not found: %s", id)
}

// UnsilenceRule unsilences an alert rule
func (m *AlertManager) UnsilenceRule(id string) error {
	if rule, ok := m.rules.Load(id); ok {
		r := rule.(*AlertRule)
		r.Silenced = false
		r.UpdatedAt = time.Now()
		m.rules.Store(id, r)
		return nil
	}
	return fmt.Errorf("alert rule not found: %s", id)
}

// handleTaskResult handles task execution results
func (m *AlertManager) handleTaskResult(msg *nats.Msg) {
	var result model.TaskResult
	if err := json.Unmarshal(msg.Data, &result); err != nil {
		m.logger.Error("Failed to unmarshal task result", zap.Error(err))
		return
	}

	// Check task failure rules
	m.rules.Range(func(key, value interface{}) bool {
		rule := value.(*AlertRule)
		if rule.Type == "task_failure" && result.Status == model.TaskStatusFailed {
			m.createAlert(rule, map[string]interface{}{
				"task_id": result.TaskID,
				"error":   result.Error,
			})
		}
		return true
	})
}

// handleResourceMetrics handles resource usage metrics
func (m *AlertManager) handleResourceMetrics(msg *nats.Msg) {
	var metrics struct {
		ExecutorID string  `json:"executor_id"`
		CPU        float64 `json:"cpu"`
		Memory     float64 `json:"memory"`
	}
	if err := json.Unmarshal(msg.Data, &metrics); err != nil {
		m.logger.Error("Failed to unmarshal resource metrics", zap.Error(err))
		return
	}

	// Check resource usage rules
	m.rules.Range(func(key, value interface{}) bool {
		rule := value.(*AlertRule)
		if rule.Type == "resource_usage" {
			switch rule.Condition {
			case "cpu_usage":
				if metrics.CPU > rule.Threshold {
					m.createAlert(rule, map[string]interface{}{
						"executor_id": metrics.ExecutorID,
						"cpu_usage":   metrics.CPU,
						"threshold":   rule.Threshold,
					})
				}
			case "memory_usage":
				if metrics.Memory > rule.Threshold {
					m.createAlert(rule, map[string]interface{}{
						"executor_id":    metrics.ExecutorID,
						"memory_usage":   metrics.Memory,
						"threshold":      rule.Threshold,
					})
				}
			}
		}
		return true
	})
}

// createAlert creates a new alert instance
func (m *AlertManager) createAlert(rule *AlertRule, details interface{}) {
	if rule.Silenced {
		return
	}

	alert := &Alert{
		RuleID:    rule.ID,
		Level:     rule.Level,
		Message:   fmt.Sprintf("Alert: %s", rule.Name),
		Details:   details,
		Status:    "firing",
		StartedAt: time.Now(),
	}

	m.alerts.Store(alert.ID, alert)

	// Send notifications
	for _, channel := range rule.Channels {
		if notifier, ok := m.channels[channel]; ok {
			if err := notifier.Send(alert); err != nil {
				m.logger.Error("Failed to send alert notification",
					zap.String("channel", channel),
					zap.Error(err))
			}
		}
	}

	// Publish alert event
	data, _ := json.Marshal(alert)
	if _, err := m.js.Publish("alert.fired", data); err != nil {
		m.logger.Error("Failed to publish alert event", zap.Error(err))
	}
}

// evaluationLoop periodically evaluates alert conditions
func (m *AlertManager) evaluationLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stop:
			return
		case <-ticker.C:
			m.evaluateTimeoutRules()
		}
	}
}

// evaluateTimeoutRules evaluates execution timeout rules
func (m *AlertManager) evaluateTimeoutRules() {
	m.rules.Range(func(key, value interface{}) bool {
		rule := value.(*AlertRule)
		if rule.Type == "execution_timeout" && !rule.Silenced {
			duration, err := time.ParseDuration(rule.Duration)
			if err != nil {
				m.logger.Error("Invalid duration in timeout rule",
					zap.String("rule_id", rule.ID),
					zap.Error(err))
				return true
			}

			// Get running tasks using consumer
			sub := fmt.Sprintf("task.status.%s", model.TaskStatusRunning)
			consumer, err := m.js.PullSubscribe(sub, "timeout-checker", nats.PullMaxWaiting(128))
			if err != nil {
				m.logger.Error("Failed to create consumer",
					zap.Error(err))
				return true
			}
			defer consumer.Unsubscribe()

			// Fetch messages
			msgs, err := consumer.Fetch(100, nats.MaxWait(time.Second))
			if err != nil && err != nats.ErrTimeout {
				m.logger.Error("Failed to fetch messages",
					zap.Error(err))
				return true
			}

			now := time.Now()
			for _, msg := range msgs {
				var task model.Task
				if err := json.Unmarshal(msg.Data, &task); err != nil {
					m.logger.Error("Failed to unmarshal task",
						zap.Error(err))
					msg.Ack()
					continue
				}

				if task.StartedAt != nil && now.Sub(*task.StartedAt) > duration {
					m.createAlert(rule, map[string]interface{}{
						"task_id":   task.ID,
						"duration":  duration.String(),
						"start_at": task.StartedAt,
					})
				}
				msg.Ack()
			}
		}
		return true
	})
}
