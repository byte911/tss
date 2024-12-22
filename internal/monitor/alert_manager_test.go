package monitor

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/t77yq/nats-project/internal/model"
	"github.com/t77yq/nats-project/internal/testutil"
)

func TestAlertManager_AddRule(t *testing.T) {
	// Setup
	logger, _ := zap.NewDevelopment()
	_, js, cleanup := testutil.StartJetStream(t)
	defer cleanup()

	manager := NewAlertManager(logger, js)

	// Test case 1: Add a valid rule
	rule1 := &model.AlertRule{
		Name:        "High CPU Usage",
		Type:        model.AlertTypeResourceUsage,
		Threshold:   80.0,
		Severity:    model.AlertSeverityWarning,
		NotifyEmail: "admin@example.com",
	}

	err := manager.AddRule(rule1)
	require.NoError(t, err)
	require.NotEmpty(t, rule1.ID)
	require.False(t, rule1.CreatedAt.IsZero())
	require.Equal(t, rule1.CreatedAt, rule1.UpdatedAt)

	// Test case 2: Add another rule
	rule2 := &model.AlertRule{
		Name:        "Task Failure",
		Type:        model.AlertTypeTaskFailure,
		Severity:    model.AlertSeverityError,
		NotifyEmail: "admin@example.com",
	}

	err = manager.AddRule(rule2)
	require.NoError(t, err)
	require.NotEmpty(t, rule2.ID)
	require.NotEqual(t, rule1.ID, rule2.ID)
}

func TestAlertManager_UpdateRule(t *testing.T) {
	// Setup
	logger, _ := zap.NewDevelopment()
	_, js, cleanup := testutil.StartJetStream(t)
	defer cleanup()

	manager := NewAlertManager(logger, js)

	// Add a rule first
	rule := &model.AlertRule{
		Name:        "High CPU Usage",
		Type:        model.AlertTypeResourceUsage,
		Threshold:   80.0,
		Severity:    model.AlertSeverityWarning,
		NotifyEmail: "admin@example.com",
	}

	err := manager.AddRule(rule)
	require.NoError(t, err)

	// Update the rule
	rule.Threshold = 90.0
	rule.Severity = model.AlertSeverityCritical
	err = manager.UpdateRule(rule)
	require.NoError(t, err)

	// Verify the update
	updated, err := manager.GetRule(rule.ID)
	require.NoError(t, err)
	require.Equal(t, 90.0, updated.Threshold)
	require.Equal(t, model.AlertSeverityCritical, updated.Severity)
	require.True(t, updated.UpdatedAt.After(updated.CreatedAt))
}

func TestAlertManager_DeleteRule(t *testing.T) {
	// Setup
	logger, _ := zap.NewDevelopment()
	_, js, cleanup := testutil.StartJetStream(t)
	defer cleanup()

	manager := NewAlertManager(logger, js)

	// Add a rule first
	rule := &model.AlertRule{
		Name:        "High CPU Usage",
		Type:        model.AlertTypeResourceUsage,
		Threshold:   80.0,
		Severity:    model.AlertSeverityWarning,
		NotifyEmail: "admin@example.com",
	}

	err := manager.AddRule(rule)
	require.NoError(t, err)

	// Delete the rule
	err = manager.DeleteRule(rule.ID)
	require.NoError(t, err)

	// Verify the deletion
	_, err = manager.GetRule(rule.ID)
	require.Error(t, err)
}

func TestAlertManager_HandleTaskResult(t *testing.T) {
	// Setup
	logger, _ := zap.NewDevelopment()
	_, js, cleanup := testutil.StartJetStream(t)
	defer cleanup()

	manager := NewAlertManager(logger, js)

	// Create stream for alerts
	_, err := js.AddStream(&nats.StreamConfig{
		Name:     "ALERTS",
		Subjects: []string{"alert.*"},
		Storage:  nats.FileStorage,
	})
	require.NoError(t, err)

	// Add a task failure rule
	rule := &model.AlertRule{
		Name:        "Task Failure",
		Type:        model.AlertTypeTaskFailure,
		Severity:    model.AlertSeverityError,
		NotifyEmail: "admin@example.com",
	}

	err = manager.AddRule(rule)
	require.NoError(t, err)

	// Subscribe to alerts
	alertReceived := make(chan struct{})
	sub, err := js.Subscribe("alert."+string(model.AlertTypeTaskFailure), func(msg *nats.Msg) {
		var alert model.Alert
		err := json.Unmarshal(msg.Data, &alert)
		require.NoError(t, err)

		require.Equal(t, rule.ID, alert.RuleID)
		require.Equal(t, model.AlertTypeTaskFailure, alert.Type)
		require.Equal(t, model.AlertSeverityError, alert.Severity)
		require.Equal(t, "test-task", alert.Data["task_id"])
		require.Equal(t, "test error", alert.Data["error"])

		close(alertReceived)
	})
	require.NoError(t, err)
	defer sub.Unsubscribe()

	// Start the manager
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()

	// Publish a task failure result
	result := struct {
		TaskID    string  `json:"task_id"`
		Status    string  `json:"status"`
		Error     string  `json:"error"`
		Duration  float64 `json:"duration"`
		CPU       float64 `json:"cpu_usage"`
		Memory    int64   `json:"memory_usage"`
		Timestamp int64   `json:"timestamp"`
	}{
		TaskID:    "test-task",
		Status:    "failed",
		Error:     "test error",
		Duration:  1.5,
		CPU:       50.0,
		Memory:    1024 * 1024 * 100,
		Timestamp: time.Now().Unix(),
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	_, err = js.Publish("task.result.test-task", data)
	require.NoError(t, err)

	// Wait for alert to be received
	select {
	case <-alertReceived:
		// Success
	case <-ctx.Done():
		t.Fatal("timeout waiting for alert")
	}
}

func TestAlertManager_TimeoutAlerts(t *testing.T) {
	// Setup
	logger, _ := zap.NewDevelopment()
	_, js, cleanup := testutil.StartJetStream(t)
	defer cleanup()

	manager := NewAlertManager(logger, js)

	// Create stream for alerts
	_, err := js.AddStream(&nats.StreamConfig{
		Name:     "ALERTS",
		Subjects: []string{"alert.*"},
		Storage:  nats.FileStorage,
	})
	require.NoError(t, err)

	// Add a timeout rule
	rule := &model.AlertRule{
		Name:        "Task Timeout",
		Type:        model.AlertTypeTimeout,
		Duration:    "1s",
		Severity:    model.AlertSeverityWarning,
		NotifyEmail: "admin@example.com",
	}

	err = manager.AddRule(rule)
	require.NoError(t, err)

	// Subscribe to timeout alerts
	alertReceived := make(chan struct{})
	sub, err := js.Subscribe("alert."+string(model.AlertTypeTimeout), func(msg *nats.Msg) {
		var alert model.Alert
		err := json.Unmarshal(msg.Data, &alert)
		require.NoError(t, err)

		require.Equal(t, rule.ID, alert.RuleID)
		require.Equal(t, model.AlertTypeTimeout, alert.Type)
		require.Equal(t, model.AlertSeverityWarning, alert.Severity)

		close(alertReceived)
	})
	require.NoError(t, err)
	defer sub.Unsubscribe()

	// Start the manager
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()

	// Create an unresolved alert
	alert := &model.Alert{
		ID:        "test-alert",
		RuleID:    rule.ID,
		Type:      model.AlertTypeTaskFailure,
		Severity:  model.AlertSeverityError,
		Message:   "Test alert",
		CreatedAt: time.Now().Add(-2 * time.Second),
	}

	manager.alerts.Store(alert.ID, alert)

	// Wait for timeout alert
	select {
	case <-alertReceived:
		// Success
	case <-ctx.Done():
		t.Fatal("timeout waiting for alert")
	}
}
