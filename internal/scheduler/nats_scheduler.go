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
	operationTimeout = 30 * time.Second
)

// NATSScheduler implements the Scheduler interface using NATS
type NATSScheduler struct {
	js              nats.JetStreamContext
	logger          *zap.Logger
	tasks           sync.Map // In-memory task cache
	cronScheduler   *CronScheduler
	depManager      *DependencyManager
}

// NewNATSScheduler creates a new NATS-based scheduler
func NewNATSScheduler(js nats.JetStreamContext, logger *zap.Logger) (*NATSScheduler, error) {
	scheduler := &NATSScheduler{
		js:           js,
		logger:       logger,
		cronScheduler: NewCronScheduler(js, logger.Named("cron")),
		depManager:   NewDependencyManager(logger.Named("deps")),
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

	// Start cron scheduler
	if err := scheduler.cronScheduler.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start cron scheduler: %w", err)
	}

	return scheduler, nil
}

func (s *NATSScheduler) setupStreams(ctx context.Context) error {
	// Create task stream with configuration
	_, err := s.js.AddStream(&nats.StreamConfig{
		Name:     "TASKS",
		Subjects: []string{"task.*"},
		Storage:  nats.FileStorage,
		MaxAge:   24 * time.Hour,
		MaxMsgs:  -1,
	}, nats.Context(ctx))

	if err != nil {
		// If stream already exists, that's okay
		if err == nats.ErrStreamNameAlreadyInUse {
			s.logger.Info("Stream already exists", zap.String("stream", "TASKS"))
			return nil
		}
		return err
	}

	s.logger.Info("Stream created successfully", zap.String("stream", "TASKS"))
	return nil
}

func (s *NATSScheduler) setupSubscribers(ctx context.Context) error {
	// Subscribe to task results
	_, err := s.js.Subscribe("task.result", func(msg *nats.Msg) {
		var result model.TaskResult
		if err := json.Unmarshal(msg.Data, &result); err != nil {
			s.logger.Error("Failed to unmarshal task result", zap.Error(err))
			return
		}
		s.updateTaskStatus(&result)
	}, nats.Context(ctx))
	return err
}

// SubmitTask submits a new task to the scheduler
func (s *NATSScheduler) SubmitTask(ctx context.Context, task *model.Task) error {
	// Add task to dependency manager
	if err := s.depManager.AddTask(task); err != nil {
		return fmt.Errorf("failed to add task to dependency manager: %w", err)
	}

	// Only publish ready tasks to NATS
	if task.Status == model.TaskStatusReady {
		data, err := json.Marshal(task)
		if err != nil {
			return fmt.Errorf("failed to marshal task: %w", err)
		}

		_, err = s.js.Publish("task.submit", data)
		if err != nil {
			return fmt.Errorf("failed to publish task: %w", err)
		}

		s.logger.Info("Task submitted", 
			zap.String("id", task.ID),
			zap.String("status", string(task.Status)))
	} else {
		s.logger.Info("Task blocked by dependencies",
			zap.String("id", task.ID),
			zap.Strings("dependencies", task.Dependencies))
	}

	// Store task in cache
	s.tasks.Store(task.ID, task)
	return nil
}

func (s *NATSScheduler) CancelTask(ctx context.Context, taskID string) error {
	if taskIface, ok := s.tasks.Load(taskID); ok {
		task := taskIface.(*model.Task)
		task.Status = model.TaskStatusCancelled
		s.tasks.Store(taskID, task)

		result := &model.TaskResult{
			TaskID:      taskID,
			Status:      model.TaskStatusCancelled,
			Error:       "Task cancelled by user",
			CompletedAt: time.Now(),
		}

		data, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("failed to marshal task result: %w", err)
		}

		_, err = s.js.Publish("task.result", data)
		if err != nil {
			return fmt.Errorf("failed to publish task result: %w", err)
		}
	}

	return nil
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
	// Update task in cache
	if taskIface, ok := s.tasks.Load(result.TaskID); ok {
		task := taskIface.(*model.Task)
		task.Status = result.Status
		task.Result = result.Result
		task.Error = result.Error
		task.CompletedAt = &result.CompletedAt
		s.tasks.Store(task.ID, task)

		// Update dependency manager
		s.depManager.UpdateTaskStatus(task.ID, result.Status)

		// If task completed successfully, check for dependent tasks
		if result.Status == model.TaskStatusComplete {
			// Get ready tasks and submit them
			readyTasks := s.depManager.GetReadyTasks()
			for _, readyTask := range readyTasks {
				if err := s.SubmitTask(context.Background(), readyTask); err != nil {
					s.logger.Error("Failed to submit ready task",
						zap.String("task_id", readyTask.ID),
						zap.Error(err))
				}
			}
		}
	}
}

// AddSchedule adds a new cron schedule
func (s *NATSScheduler) AddSchedule(ctx context.Context, schedule *model.CronSchedule) error {
	return s.cronScheduler.AddSchedule(ctx, schedule)
}

// RemoveSchedule removes a cron schedule
func (s *NATSScheduler) RemoveSchedule(id string) error {
	return s.cronScheduler.RemoveSchedule(id)
}

// GetSchedule gets a cron schedule by ID
func (s *NATSScheduler) GetSchedule(id string) (*model.CronSchedule, error) {
	return s.cronScheduler.GetSchedule(id)
}

// ListSchedules lists all cron schedules
func (s *NATSScheduler) ListSchedules() []*model.CronSchedule {
	return s.cronScheduler.ListSchedules()
}
