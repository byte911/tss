package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/t77yq/nats-project/internal/model"
	"github.com/t77yq/nats-project/internal/testutil"
)

func TestNATSScheduler(t *testing.T) {
	// Setup test environment
	js, cleanup := testutil.SetupJetStream(t)
	defer cleanup()

	logger := zap.NewNop()
	scheduler, err := NewNATSScheduler(js, logger)
	require.NoError(t, err)

	t.Run("Setup", func(t *testing.T) {
		// Verify stream creation
		stream, err := js.StreamInfo("TASKS")
		require.NoError(t, err)
		assert.Equal(t, "TASKS", stream.Config.Name)
		assert.Equal(t, []string{"task.*"}, stream.Config.Subjects)
	})

	t.Run("Submit Task", func(t *testing.T) {
		task := &model.Task{
			ID:          uuid.New().String(),
			Name:        "test-task",
			Description: "Test task submission",
			Type:        "test",
			Priority:    model.TaskPriorityNormal,
			Status:      model.TaskStatusReady,
			ScheduledAt: time.Now(),
			CreatedAt:   time.Now(),
		}

		err := scheduler.SubmitTask(context.Background(), task)
		require.NoError(t, err)

		// Verify task was stored
		storedTask, err := scheduler.GetTaskStatus(context.Background(), task.ID)
		require.NoError(t, err)
		assert.Equal(t, task.ID, storedTask.ID)
		assert.Equal(t, task.Name, storedTask.Name)
		assert.Equal(t, task.Status, storedTask.Status)
	})

	t.Run("Submit Task with Dependencies", func(t *testing.T) {
		// Create parent task
		parentTask := &model.Task{
			ID:          uuid.New().String(),
			Name:        "parent-task",
			Description: "Parent task",
			Type:        "test",
			Priority:    model.TaskPriorityNormal,
			Status:      model.TaskStatusReady,
			ScheduledAt: time.Now(),
			CreatedAt:   time.Now(),
		}

		err := scheduler.SubmitTask(context.Background(), parentTask)
		require.NoError(t, err)

		// Create child task with dependency
		childTask := &model.Task{
			ID:           uuid.New().String(),
			Name:         "child-task",
			Description:  "Child task with dependency",
			Type:         "test",
			Priority:     model.TaskPriorityNormal,
			Status:       model.TaskStatusPending,
			Dependencies: []string{parentTask.ID},
			ScheduledAt:  time.Now(),
			CreatedAt:    time.Now(),
		}

		err = scheduler.SubmitTask(context.Background(), childTask)
		require.NoError(t, err)

		// Verify child task is blocked
		storedTask, err := scheduler.GetTaskStatus(context.Background(), childTask.ID)
		require.NoError(t, err)
		assert.Equal(t, model.TaskStatusBlocked, storedTask.Status)
	})

	t.Run("Cancel Task", func(t *testing.T) {
		task := &model.Task{
			ID:          uuid.New().String(),
			Name:        "cancel-test-task",
			Description: "Test task cancellation",
			Type:        "test",
			Priority:    model.TaskPriorityNormal,
			Status:      model.TaskStatusReady,
			ScheduledAt: time.Now(),
			CreatedAt:   time.Now(),
		}

		err := scheduler.SubmitTask(context.Background(), task)
		require.NoError(t, err)

		err = scheduler.CancelTask(context.Background(), task.ID)
		require.NoError(t, err)

		// Verify task was cancelled
		storedTask, err := scheduler.GetTaskStatus(context.Background(), task.ID)
		require.NoError(t, err)
		assert.Equal(t, model.TaskStatusCancelled, storedTask.Status)
	})

	t.Run("List Tasks by Priority", func(t *testing.T) {
		// Create tasks with different priorities
		tasks := []*model.Task{
			{
				ID:          uuid.New().String(),
				Name:        "high-priority-task",
				Description: "High priority task",
				Type:        "test",
				Priority:    model.TaskPriorityHigh,
				Status:      model.TaskStatusReady,
				ScheduledAt: time.Now(),
				CreatedAt:   time.Now(),
			},
			{
				ID:          uuid.New().String(),
				Name:        "normal-priority-task",
				Description: "Normal priority task",
				Type:        "test",
				Priority:    model.TaskPriorityNormal,
				Status:      model.TaskStatusReady,
				ScheduledAt: time.Now(),
				CreatedAt:   time.Now(),
			},
			{
				ID:          uuid.New().String(),
				Name:        "low-priority-task",
				Description: "Low priority task",
				Type:        "test",
				Priority:    model.TaskPriorityLow,
				Status:      model.TaskStatusReady,
				ScheduledAt: time.Now(),
				CreatedAt:   time.Now(),
			},
		}

		for _, task := range tasks {
			err := scheduler.SubmitTask(context.Background(), task)
			require.NoError(t, err)
		}

		// List high priority tasks
		filters := TaskFilters{
			Priority: []model.TaskPriority{model.TaskPriorityHigh},
		}

		highPriorityTasks, err := scheduler.ListTasks(context.Background(), filters)
		require.NoError(t, err)
		assert.Len(t, highPriorityTasks, 1)
		assert.Equal(t, model.TaskPriorityHigh, highPriorityTasks[0].Priority)
	})
}
