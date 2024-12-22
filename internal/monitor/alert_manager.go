package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"github.com/t77yq/nats-project/internal/model"
)

// NotificationChannel represents a channel for sending alert notifications
type NotificationChannel interface {
	Send(alert *model.Alert) error
}

// AlertManager manages alert rules and notifications
type AlertManager struct {
	logger    *zap.Logger
	js        nats.JetStreamContext
	rules     sync.Map
	alerts    sync.Map
	channels  map[string]NotificationChannel
	stop      chan struct{}
	sub       *nats.Subscription
}

// NewAlertManager creates a new alert manager
func NewAlertManager(logger *zap.Logger, js nats.JetStreamContext) *AlertManager {
	return &AlertManager{
		logger:   logger,
		js:       js,
		channels: make(map[string]NotificationChannel),
		stop:     make(chan struct{}),
	}
}

// Start starts the alert manager
func (m *AlertManager) Start(ctx context.Context) error {
	// Create streams for alerts
	stream, err := m.js.StreamInfo("ALERTS")
	if err != nil && err != nats.ErrStreamNotFound {
		return fmt.Errorf("failed to get stream info: %w", err)
	}

	if stream == nil {
		// Create the stream
		_, err = m.js.AddStream(&nats.StreamConfig{
			Name:     "ALERTS",
			Subjects: []string{"alert.*"},
			Storage:  nats.FileStorage,
		})
		if err != nil {
			return fmt.Errorf("failed to create stream: %w", err)
		}
	}

	// Subscribe to task results
	sub, err := m.js.Subscribe("task.result.*", m.handleTaskResult)
	if err != nil {
		return fmt.Errorf("failed to subscribe to task results: %w", err)
	}
	m.sub = sub

	// Start evaluation loop
	go m.evaluationLoop(ctx)

	m.logger.Info("Alert manager started")

	return nil
}

// Stop stops the alert manager
func (m *AlertManager) Stop() {
	if m.sub != nil {
		m.sub.Unsubscribe()
	}
	close(m.stop)
}

// GetRule returns a rule by ID
func (m *AlertManager) GetRule(id string) (*model.AlertRule, error) {
	value, ok := m.rules.Load(id)
	if !ok {
		return nil, fmt.Errorf("rule not found: %s", id)
	}
	return value.(*model.AlertRule), nil
}

// AddRule adds a new alert rule
func (m *AlertManager) AddRule(rule *model.AlertRule) error {
	if rule.ID == "" {
		rule.ID = uuid.New().String()
	}
	rule.CreatedAt = time.Now()
	rule.UpdatedAt = rule.CreatedAt
	m.rules.Store(rule.ID, rule)
	return nil
}

// UpdateRule updates an existing alert rule
func (m *AlertManager) UpdateRule(rule *model.AlertRule) error {
	if _, ok := m.rules.Load(rule.ID); !ok {
		return fmt.Errorf("rule not found: %s", rule.ID)
	}
	rule.UpdatedAt = time.Now()
	m.rules.Store(rule.ID, rule)
	return nil
}

// DeleteRule deletes an alert rule
func (m *AlertManager) DeleteRule(id string) error {
	if _, ok := m.rules.Load(id); !ok {
		return fmt.Errorf("rule not found: %s", id)
	}
	m.rules.Delete(id)
	return nil
}

// createAlert creates and publishes a new alert
func (m *AlertManager) createAlert(rule *model.AlertRule, data map[string]interface{}) error {
	alert := &model.Alert{
		ID:        uuid.New().String(),
		RuleID:    rule.ID,
		Type:      rule.Type,
		Severity:  rule.Severity,
		Message:   fmt.Sprintf("Alert triggered for rule: %s", rule.Name),
		Data:      data,
		CreatedAt: time.Now(),
	}

	alertData, err := json.Marshal(alert)
	if err != nil {
		return fmt.Errorf("failed to marshal alert: %w", err)
	}

	_, err = m.js.Publish("alert."+string(alert.Type), alertData)
	if err != nil {
		return fmt.Errorf("failed to publish alert: %w", err)
	}

	m.logger.Info("Alert created",
		zap.String("id", alert.ID),
		zap.String("rule_id", alert.RuleID),
		zap.String("type", string(alert.Type)),
		zap.String("severity", string(alert.Severity)))

	return nil
}

// handleTaskResult handles task execution results
func (m *AlertManager) handleTaskResult(msg *nats.Msg) {
	var result struct {
		TaskID    string  `json:"task_id"`
		Status    string  `json:"status"`
		Error     string  `json:"error,omitempty"`
		Duration  float64 `json:"duration"`
		CPU       float64 `json:"cpu_usage"`
		Memory    int64   `json:"memory_usage"`
		Timestamp int64   `json:"timestamp"`
	}

	if err := json.Unmarshal(msg.Data, &result); err != nil {
		m.logger.Error("Failed to unmarshal task result", zap.Error(err))
		return
	}

	// Check for task failure
	if result.Status == "failed" {
		m.rules.Range(func(key, value interface{}) bool {
			rule := value.(*model.AlertRule)
			if rule.Type == model.AlertTypeTaskFailure {
				m.createAlert(rule, map[string]interface{}{
					"task_id": result.TaskID,
					"error":   result.Error,
				})
			}
			return true
		})
	}

	// Check for resource usage
	m.rules.Range(func(key, value interface{}) bool {
		rule := value.(*model.AlertRule)
		if rule.Type == model.AlertTypeResourceUsage {
			if result.CPU > rule.Threshold {
				m.createAlert(rule, map[string]interface{}{
					"task_id":   result.TaskID,
					"cpu_usage": result.CPU,
				})
			}
			if float64(result.Memory) > rule.Threshold {
				m.createAlert(rule, map[string]interface{}{
					"task_id":      result.TaskID,
					"memory_usage": result.Memory,
				})
			}
		}
		return true
	})
}

// evaluationLoop periodically evaluates alert conditions
func (m *AlertManager) evaluationLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.evaluateTimeoutAlerts()
		}
	}
}

// evaluateTimeoutAlerts checks for task execution timeouts
func (m *AlertManager) evaluateTimeoutAlerts() {
	m.rules.Range(func(key, value interface{}) bool {
		rule := value.(*model.AlertRule)
		if rule.Type == model.AlertTypeTimeout {
			duration, err := time.ParseDuration(rule.Duration)
			if err != nil {
				m.logger.Error("Invalid duration in timeout rule",
					zap.String("rule_id", rule.ID),
					zap.Error(err))
				return true
			}

			m.alerts.Range(func(key, value interface{}) bool {
				alert := value.(*model.Alert)
				if alert.ResolvedAt == nil && time.Since(alert.CreatedAt) > duration {
					m.createAlert(rule, map[string]interface{}{
						"alert_id":     alert.ID,
						"elapsed_time": time.Since(alert.CreatedAt).String(),
					})
				}
				return true
			})
		}
		return true
	})
}
