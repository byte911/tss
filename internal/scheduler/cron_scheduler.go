package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"github.com/t77yq/nats-project/internal/model"
)

// CronScheduler manages scheduled tasks
type CronScheduler struct {
	logger    *zap.Logger
	js        nats.JetStreamContext
	cron      *cron.Cron
	schedules sync.Map
	entryIDs  sync.Map
}

// cronLogger adapts zap.Logger to cron.Logger
type cronLogger struct {
	logger *zap.Logger
}

func (l *cronLogger) Info(msg string, keysAndValues ...interface{}) {
	l.logger.Info(msg)
}

func (l *cronLogger) Error(err error, msg string, keysAndValues ...interface{}) {
	l.logger.Error(msg, zap.Error(err))
}

// NewCronScheduler creates a new scheduler
func NewCronScheduler(js nats.JetStreamContext, logger *zap.Logger) *CronScheduler {
	cronLogger := &cronLogger{logger: logger.Named("cron")}
	cronOptions := []cron.Option{
		cron.WithSeconds(),
		cron.WithChain(cron.Recover(cronLogger)),
	}

	return &CronScheduler{
		logger: logger,
		js:     js,
		cron:   cron.New(cronOptions...),
	}
}

// Start starts the scheduler
func (s *CronScheduler) Start(ctx context.Context) error {
	// Create stream for schedules
	_, err := s.js.StreamInfo("SCHEDULES")
	if err != nil {
		if err != nats.ErrStreamNotFound {
			return fmt.Errorf("failed to get stream info: %w", err)
		}

		// Create the stream if it doesn't exist
		_, err = s.js.AddStream(&nats.StreamConfig{
			Name:     "SCHEDULES",
			Subjects: []string{"schedule.*"},
			Storage:  nats.FileStorage,
			MaxAge:   24 * time.Hour,
			MaxMsgs:  -1,
		})
		if err != nil {
			return fmt.Errorf("failed to create stream: %w", err)
		}
		s.logger.Info("Created schedule stream", zap.String("name", "SCHEDULES"))
	} else {
		s.logger.Info("Using existing schedule stream", zap.String("name", "SCHEDULES"))
	}

	s.cron.Start()
	return s.subscribeToCommands(ctx)
}

// Stop stops the scheduler
func (s *CronScheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
}

// AddSchedule adds a new schedule
func (s *CronScheduler) AddSchedule(ctx context.Context, schedule *model.CronSchedule) error {
	if schedule.ID == "" {
		schedule.ID = uuid.New().String()
	}
	if schedule.Status == "" {
		schedule.Status = model.TaskStatusPending
	}
	if schedule.CreatedAt.IsZero() {
		schedule.CreatedAt = time.Now()
	}
	schedule.UpdatedAt = time.Now()

	specParser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	spec, err := specParser.Parse(schedule.Expression)
	if err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}

	s.schedules.Store(schedule.ID, schedule)

	entryID, err := s.cron.AddJob(schedule.Expression, &cronJob{
		scheduler: s,
		schedule:  schedule,
	})
	if err != nil {
		s.schedules.Delete(schedule.ID)
		return fmt.Errorf("failed to add cron job: %w", err)
	}

	s.entryIDs.Store(schedule.ID, entryID)

	next := spec.Next(time.Now())
	schedule.NextRunTime = &next

	s.logger.Info("Added schedule",
		zap.String("id", schedule.ID),
		zap.String("name", schedule.Name),
		zap.String("expression", schedule.Expression),
		zap.Time("next_run", next))

	return nil
}

// RemoveSchedule removes a schedule
func (s *CronScheduler) RemoveSchedule(id string) error {
	entryIDVal, ok := s.entryIDs.Load(id)
	if !ok {
		return fmt.Errorf("schedule not found: %s", id)
	}

	s.cron.Remove(entryIDVal.(cron.EntryID))
	s.entryIDs.Delete(id)
	s.schedules.Delete(id)

	s.logger.Info("Removed schedule", zap.String("id", id))
	return nil
}

// GetSchedule gets a schedule by ID
func (s *CronScheduler) GetSchedule(id string) (*model.CronSchedule, error) {
	val, ok := s.schedules.Load(id)
	if !ok {
		return nil, fmt.Errorf("schedule not found: %s", id)
	}
	return val.(*model.CronSchedule), nil
}

// ListSchedules lists all schedules
func (s *CronScheduler) ListSchedules() []*model.CronSchedule {
	var schedules []*model.CronSchedule
	s.schedules.Range(func(key, value interface{}) bool {
		schedules = append(schedules, value.(*model.CronSchedule))
		return true
	})
	return schedules
}

// subscribeToCommands subscribes to schedule management commands
func (s *CronScheduler) subscribeToCommands(ctx context.Context) error {
	// Subscribe to schedule.add with a durable consumer
	if _, err := s.js.Subscribe("schedule.add", func(msg *nats.Msg) {
		var schedule model.CronSchedule
		if err := json.Unmarshal(msg.Data, &schedule); err != nil {
			s.logger.Error("Failed to unmarshal schedule", zap.Error(err))
			return
		}

		if err := s.AddSchedule(ctx, &schedule); err != nil {
			s.logger.Error("Failed to add schedule", zap.Error(err))
			return
		}
	}, nats.Durable("schedule-add-consumer")); err != nil {
		return fmt.Errorf("failed to subscribe to schedule.add: %w", err)
	}

	// Subscribe to schedule.remove with a durable consumer
	if _, err := s.js.Subscribe("schedule.remove", func(msg *nats.Msg) {
		var id string
		if err := json.Unmarshal(msg.Data, &id); err != nil {
			s.logger.Error("Failed to unmarshal schedule ID", zap.Error(err))
			return
		}

		if err := s.RemoveSchedule(id); err != nil {
			s.logger.Error("Failed to remove schedule", zap.Error(err))
			return
		}
	}, nats.Durable("schedule-remove-consumer")); err != nil {
		return fmt.Errorf("failed to subscribe to schedule.remove: %w", err)
	}

	return nil
}

// cronJob implements cron.Job interface
type cronJob struct {
	scheduler *CronScheduler
	schedule  *model.CronSchedule
}

// Run implements cron.Job
func (j *cronJob) Run() {
	now := time.Now()
	j.schedule.LastRunTime = &now

	specParser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	spec, err := specParser.Parse(j.schedule.Expression)
	if err != nil {
		j.scheduler.logger.Error("Failed to parse cron expression",
			zap.String("id", j.schedule.ID),
			zap.Error(err))
		return
	}

	next := spec.Next(now)
	j.schedule.NextRunTime = &next

	task := &model.Task{
		ID:      uuid.New().String(),
		Name:    j.schedule.Name,
		Payload: j.schedule.Payload,
	}

	data, err := json.Marshal(task)
	if err != nil {
		j.scheduler.logger.Error("Failed to marshal task",
			zap.String("id", j.schedule.ID),
			zap.Error(err))
		return
	}

	if _, err := j.scheduler.js.Publish("task.submit", data); err != nil {
		j.scheduler.logger.Error("Failed to publish task",
			zap.String("id", j.schedule.ID),
			zap.Error(err))
		return
	}

	j.scheduler.logger.Info("Executed schedule",
		zap.String("id", j.schedule.ID),
		zap.String("name", j.schedule.Name),
		zap.Time("executed_at", now),
		zap.Time("next_run", next))
}
