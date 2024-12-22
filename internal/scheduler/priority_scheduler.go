package scheduler

import (
	"container/heap"
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/t77yq/nats-project/internal/model"
)

// TaskQueue implements a priority queue for tasks
type TaskQueue struct {
	tasks []*model.Task
	mu    sync.RWMutex
}

// Len returns the length of the queue
func (q *TaskQueue) Len() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.tasks)
}

// Less compares two tasks by their priority and creation time
func (q *TaskQueue) Less(i, j int) bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	
	// First compare by priority
	if q.tasks[i].Priority != q.tasks[j].Priority {
		switch q.tasks[i].Priority {
		case model.TaskPriorityHigh:
			return true
		case model.TaskPriorityNormal:
			return q.tasks[j].Priority == model.TaskPriorityLow
		case model.TaskPriorityLow:
			return false
		}
	}
	
	// If priorities are equal, compare by creation time
	return q.tasks[i].CreatedAt.Before(q.tasks[j].CreatedAt)
}

// Swap swaps two tasks in the queue
func (q *TaskQueue) Swap(i, j int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.tasks[i], q.tasks[j] = q.tasks[j], q.tasks[i]
}

// Push adds a task to the queue
func (q *TaskQueue) Push(x interface{}) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.tasks = append(q.tasks, x.(*model.Task))
}

// Pop removes and returns the highest priority task
func (q *TaskQueue) Pop() interface{} {
	q.mu.Lock()
	defer q.mu.Unlock()
	old := q.tasks
	n := len(old)
	if n == 0 {
		return nil
	}
	item := old[n-1]
	q.tasks = old[0 : n-1]
	return item
}

// PriorityScheduler implements priority-based task scheduling
type PriorityScheduler struct {
	logger    *zap.Logger
	queues    map[model.TaskPriority]*TaskQueue
	scheduler Scheduler
	mu        sync.RWMutex
	stop      chan struct{}
}

// NewPriorityScheduler creates a new priority scheduler
func NewPriorityScheduler(scheduler Scheduler, logger *zap.Logger) *PriorityScheduler {
	ps := &PriorityScheduler{
		logger:    logger.Named("priority-scheduler"),
		queues:    make(map[model.TaskPriority]*TaskQueue),
		scheduler: scheduler,
		stop:      make(chan struct{}),
	}

	// Initialize queues for each priority level
	ps.queues[model.TaskPriorityHigh] = &TaskQueue{}
	ps.queues[model.TaskPriorityNormal] = &TaskQueue{}
	ps.queues[model.TaskPriorityLow] = &TaskQueue{}

	// Initialize priority queues
	for _, q := range ps.queues {
		heap.Init(q)
	}

	return ps
}

// Start starts the priority scheduler
func (s *PriorityScheduler) Start(ctx context.Context) error {
	s.logger.Info("Starting priority scheduler")

	// Start scheduling goroutine
	go s.scheduleLoop(ctx)

	return nil
}

// Stop stops the priority scheduler
func (s *PriorityScheduler) Stop() {
	s.logger.Info("Stopping priority scheduler")
	close(s.stop)
}

// SubmitTask submits a task to the appropriate priority queue
func (s *PriorityScheduler) SubmitTask(ctx context.Context, task *model.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get queue for task priority
	queue, ok := s.queues[task.Priority]
	if !ok {
		s.logger.Error("Invalid task priority",
			zap.String("task_id", task.ID),
			zap.String("priority", string(task.Priority)))
		return ErrInvalidPriority
	}

	// Add task to queue
	heap.Push(queue, task)

	s.logger.Info("Task submitted to priority queue",
		zap.String("task_id", task.ID),
		zap.String("priority", string(task.Priority)),
		zap.Int("queue_length", queue.Len()))

	return nil
}

// scheduleLoop runs the scheduling loop
func (s *PriorityScheduler) scheduleLoop(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case <-ticker.C:
			s.scheduleTasks(ctx)
		}
	}
}

// scheduleTasks schedules tasks from priority queues
func (s *PriorityScheduler) scheduleTasks(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check queues in priority order
	priorities := []model.TaskPriority{
		model.TaskPriorityHigh,
		model.TaskPriorityNormal,
		model.TaskPriorityLow,
	}

	for _, priority := range priorities {
		queue := s.queues[priority]
		for queue.Len() > 0 {
			// Get next task
			task := heap.Pop(queue).(*model.Task)

			// Submit task to underlying scheduler
			if err := s.scheduler.SubmitTask(ctx, task); err != nil {
				s.logger.Error("Failed to submit task",
					zap.String("task_id", task.ID),
					zap.Error(err))
				
				// Re-queue task
				heap.Push(queue, task)
				break
			}

			s.logger.Info("Task scheduled",
				zap.String("task_id", task.ID),
				zap.String("priority", string(priority)))
		}
	}
}
