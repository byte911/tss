package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/smtp"
	"time"

	"go.uber.org/zap"

	"github.com/t77yq/nats-project/internal/model"
)

// NotificationType defines the type of notification
type NotificationType string

const (
	NotificationEmail NotificationType = "email"
	NotificationSMS  NotificationType = "sms"
	NotificationPush NotificationType = "push"
)

// NotificationPayload represents the payload for notification tasks
type NotificationPayload struct {
	Type      NotificationType     `json:"type"`
	Template  string              `json:"template"`
	Data      interface{}         `json:"data"`
	Recipients []string           `json:"recipients"`
	Options    map[string]string  `json:"options"`
}

// NotificationHandler handles notification sending
type NotificationHandler struct {
	logger *zap.Logger
	config NotificationConfig
}

// NotificationConfig holds configuration for different notification channels
type NotificationConfig struct {
	Email struct {
		Host     string
		Port     int
		Username string
		Password string
		From     string
	}
	SMS struct {
		Provider string
		APIKey   string
		From     string
	}
	Push struct {
		Provider string
		APIKey   string
	}
}

// NewNotificationHandler creates a new notification handler
func NewNotificationHandler(logger *zap.Logger, config NotificationConfig) *NotificationHandler {
	return &NotificationHandler{
		logger: logger,
		config: config,
	}
}

// Execute sends the notification
func (h *NotificationHandler) Execute(ctx context.Context, task *model.Task) (*model.TaskResult, error) {
	var payload NotificationPayload
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	h.logger.Info("Sending notification",
		zap.String("type", string(payload.Type)),
		zap.Int("recipients", len(payload.Recipients)))

	var err error
	switch payload.Type {
	case NotificationEmail:
		err = h.sendEmail(ctx, payload)
	case NotificationSMS:
		err = h.sendSMS(ctx, payload)
	case NotificationPush:
		err = h.sendPush(ctx, payload)
	default:
		return nil, fmt.Errorf("unsupported notification type: %s", payload.Type)
	}

	if err != nil {
		return &model.TaskResult{
			TaskID:      task.ID,
			Status:      model.TaskStatusFailed,
			Error:       err.Error(),
			CompletedAt: time.Now(),
		}, nil
	}

	return &model.TaskResult{
		TaskID:      task.ID,
		Status:      model.TaskStatusCompleted,
		Result:      []byte(fmt.Sprintf("Notification sent to %d recipients", len(payload.Recipients))),
		CompletedAt: time.Now(),
	}, nil
}

func (h *NotificationHandler) sendEmail(ctx context.Context, payload NotificationPayload) error {
	// Create SMTP auth
	auth := smtp.PlainAuth("",
		h.config.Email.Username,
		h.config.Email.Password,
		h.config.Email.Host)

	// Format email message
	msg := fmt.Sprintf("From: %s\r\n"+
		"To: %s\r\n"+
		"Subject: Notification\r\n"+
		"Content-Type: text/html; charset=UTF-8\r\n"+
		"\r\n"+
		"%s\r\n",
		h.config.Email.From,
		payload.Recipients[0],
		payload.Template)

	// Send email
	addr := fmt.Sprintf("%s:%d", h.config.Email.Host, h.config.Email.Port)
	return smtp.SendMail(addr, auth, h.config.Email.From, payload.Recipients, []byte(msg))
}

func (h *NotificationHandler) sendSMS(ctx context.Context, payload NotificationPayload) error {
	// TODO: Implement SMS sending using a provider (e.g., Twilio)
	return fmt.Errorf("SMS notifications not implemented")
}

func (h *NotificationHandler) sendPush(ctx context.Context, payload NotificationPayload) error {
	// TODO: Implement push notifications using a provider (e.g., Firebase)
	return fmt.Errorf("Push notifications not implemented")
}
