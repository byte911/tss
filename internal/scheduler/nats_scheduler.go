package scheduler

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

const (
	taskStreamName     = "TASKS"
	taskSubmitSubject  = "task.submit"
	taskStatusSubject  = "task.status"
	taskResultSubject  = "task.result"
	streamMaxAge       = 24 * time.Hour  // Keep messages for 24 hours
	streamMaxMsgs      = -1             // Unlimited messages
	operationTimeout   = 30 * time.Second
)

// NATSScheduler implements the Scheduler interface using NATS
type NATSScheduler struct {
	js      nats.JetStreamContext
	logger  *zap.Logger
	tasks   sync.Map // In-memory task cache
}

// NewNATSScheduler creates a new NATS-based scheduler
func NewNATSScheduler(js nats.JetStreamContext, logger *zap.Logger) (*NATSScheduler, error) {
	scheduler := &NATSScheduler{
		js:     js,
		logger: logger,
	}

	// Setup NATS streams with timeout context
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()

	if err := scheduler.setupStreams(ctx); err != nil {
		return nil, fmt.Errorf("failed to setup streams: %w", err)
	}

	// Setup subscribers with timeout context
	if err := scheduler.setupSubscribers(ctx); err != nil {
		return nil, fmt.Errorf("failed to setup subscribers: %w", err)
	}

	return scheduler, nil
}

func (s *NATSScheduler) setupStreams(ctx context.Context) error {
	// Create task stream with configuration
	_, err := s.js.AddStream(&nats.StreamConfig{
		Name:     taskStreamName,
		Subjects: []string{"task.*"},
		Storage:  nats.FileStorage,
		MaxAge:   streamMaxAge,
		MaxMsgs:  streamMaxMsgs,
	}, nats.Context(ctx))

	if err != nil {
		// If stream already exists, that's okay
		if err == nats.ErrStreamNameAlreadyInUse {
			s.logger.Info("Stream already exists", zap.String("stream", taskStreamName))
			return nil
		}
		return err
	}

	s.logger.Info("Stream created successfully", zap.String("stream", taskStreamName))
	return nil
}

func (s *NATSScheduler) setupSubscribers(ctx context.Context) error {
	// Subscribe to task results
	_, err := s.js.Subscribe(taskResultSubject, func(msg *nats.Msg) {
		var result model.TaskResult
		if err := json.Unmarshal(msg.Data, &result); err != nil {
			s.logger.Error("Failed to unmarshal task result", zap.Error(err))
			return
		}
		s.updateTaskStatus(&result)
	}, nats.Context(ctx))
	return err
}

func (s *NATSScheduler) SubmitTask(ctx context.Context, task *model.Task) error {
	// Set initial task state
	task.Status = model.TaskStatusPending
	task.CreatedAt = time.Now()
	if task.ScheduledAt.IsZero() {
		task.ScheduledAt = task.CreatedAt
	}

	// Store task in memory
	s.tasks.Store(task.ID, task)

	// Publish task to NATS
	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	_, err = s.js.Publish(taskSubmitSubject, data)
	if err != nil {
		return fmt.Errorf("failed to publish task: %w", err)
	}

	return nil
}

func (s *NATSScheduler) CancelTask(ctx context.Context, taskID string) error {
	task, err := s.GetTaskStatus(ctx, taskID)
	if err != nil {
		return err
	}

	if task.Status != model.TaskStatusPending && task.Status != model.TaskStatusRunning {
		return fmt.Errorf("task %s cannot be canceled in status %s", taskID, task.Status)
	}

	task.Status = model.TaskStatusCanceled
	s.tasks.Store(taskID, task)

	// Publish cancel event
	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	_, err = s.js.Publish(fmt.Sprintf("task.cancel.%s", taskID), data)
	return err
}

func (s *NATSScheduler) GetTaskStatus(ctx context.Context, taskID string) (*model.Task, error) {
	if task, ok := s.tasks.Load(taskID); ok {
		return task.(*model.Task), nil
	}
	return nil, fmt.Errorf("task %s not found", taskID)
}

func (s *NATSScheduler) ListTasks(ctx context.Context, filters TaskFilters) ([]*model.Task, error) {
	var tasks []*model.Task

	s.tasks.Range(func(key, value interface{}) bool {
		task := value.(*model.Task)
		if s.matchesFilters(task, filters) {
			tasks = append(tasks, task)
		}
		return true
	})

	return tasks, nil
}

func (s *NATSScheduler) matchesFilters(task *model.Task, filters TaskFilters) bool {
	// Implement filter matching logic
	// This is a simplified version
	if len(filters.Status) > 0 {
		statusMatch := false
		for _, status := range filters.Status {
			if task.Status == status {
				statusMatch = true
				break
			}
		}
		if !statusMatch {
			return false
		}
	}

	if len(filters.Priority) > 0 {
		priorityMatch := false
		for _, priority := range filters.Priority {
			if task.Priority == priority {
				priorityMatch = true
				break
			}
		}
		if !priorityMatch {
			return false
		}
	}

	return true
}

func (s *NATSScheduler) updateTaskStatus(result *model.TaskResult) {
	if task, ok := s.tasks.Load(result.TaskID); ok {
		t := task.(*model.Task)
		t.Status = result.Status
		t.Result = result.Result
		t.ErrorMessage = result.Error
		t.CompletedAt = &result.CompletedAt
		t.ExecutorID = result.ExecutorID
		s.tasks.Store(result.TaskID, t)
	}
}
