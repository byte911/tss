package scheduler

import (
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/t77yq/nats-project/internal/model"
)

// DependencyManager manages task dependencies
type DependencyManager struct {
	logger *zap.Logger
	tasks  sync.Map // Map of task ID to Task
}

// NewDependencyManager creates a new dependency manager
func NewDependencyManager(logger *zap.Logger) *DependencyManager {
	return &DependencyManager{
		logger: logger,
	}
}

// AddTask adds a task to the dependency manager
func (m *DependencyManager) AddTask(task *model.Task) error {
	// Store task
	m.tasks.Store(task.ID, task)

	// If task has dependencies, update dependent tasks list for each dependency
	if len(task.Dependencies) > 0 {
		task.Status = model.TaskStatusPending
		for _, depID := range task.Dependencies {
			if dep, ok := m.tasks.Load(depID); ok {
				depTask := dep.(*model.Task)
				if depTask.DependentTasks == nil {
					depTask.DependentTasks = make([]string, 0)
				}
				depTask.DependentTasks = append(depTask.DependentTasks, task.ID)
				m.tasks.Store(depID, depTask)
			} else {
				// If dependency doesn't exist, mark task as blocked
				task.Status = model.TaskStatusBlocked
				m.logger.Warn("Dependency not found",
					zap.String("task_id", task.ID),
					zap.String("dependency_id", depID))
			}
		}
	}

	return nil
}

// RemoveTask removes a task from the dependency manager
func (m *DependencyManager) RemoveTask(taskID string) error {
	if task, ok := m.tasks.Load(taskID); ok {
		// Remove task from dependencies' dependent tasks list
		t := task.(*model.Task)
		for _, depID := range t.Dependencies {
			if dep, ok := m.tasks.Load(depID); ok {
				depTask := dep.(*model.Task)
				for i, id := range depTask.DependentTasks {
					if id == taskID {
						depTask.DependentTasks = append(depTask.DependentTasks[:i], depTask.DependentTasks[i+1:]...)
						break
					}
				}
				m.tasks.Store(depID, depTask)
			}
		}

		// Remove task from dependent tasks' dependencies list
		for _, depID := range t.DependentTasks {
			if dep, ok := m.tasks.Load(depID); ok {
				depTask := dep.(*model.Task)
				for i, id := range depTask.Dependencies {
					if id == taskID {
						depTask.Dependencies = append(depTask.Dependencies[:i], depTask.Dependencies[i+1:]...)
						break
					}
				}
				m.tasks.Store(depID, depTask)
			}
		}

		m.tasks.Delete(taskID)
		return nil
	}
	return fmt.Errorf("task %s not found", taskID)
}

// UpdateTaskStatus updates a task's status and processes dependent tasks
func (m *DependencyManager) UpdateTaskStatus(taskID string, status model.TaskStatus) error {
	if task, ok := m.tasks.Load(taskID); ok {
		t := task.(*model.Task)
		t.Status = status
		m.tasks.Store(taskID, t)

		// If task is completed, process dependent tasks
		if status == model.TaskStatusCompleted {
			for _, depID := range t.DependentTasks {
				if dep, ok := m.tasks.Load(depID); ok {
					depTask := dep.(*model.Task)
					allDepsCompleted := true

					// Check if all dependencies are completed
					for _, id := range depTask.Dependencies {
						if d, ok := m.tasks.Load(id); ok {
							dt := d.(*model.Task)
							if dt.Status != model.TaskStatusCompleted {
								allDepsCompleted = false
								break
							}
						} else {
							allDepsCompleted = false
							break
						}
					}

					// If all dependencies are completed, mark task as ready
					if allDepsCompleted {
						depTask.Status = model.TaskStatusReady
						m.tasks.Store(depID, depTask)
						m.logger.Info("Task ready for execution",
							zap.String("task_id", depID))
					}
				}
			}
		}

		return nil
	}
	return fmt.Errorf("task %s not found", taskID)
}

// GetTask gets a task by ID
func (m *DependencyManager) GetTask(taskID string) (*model.Task, error) {
	if task, ok := m.tasks.Load(taskID); ok {
		return task.(*model.Task), nil
	}
	return nil, fmt.Errorf("task %s not found", taskID)
}

// GetDependentTasks gets all tasks that depend on the given task
func (m *DependencyManager) GetDependentTasks(taskID string) ([]*model.Task, error) {
	if task, ok := m.tasks.Load(taskID); ok {
		t := task.(*model.Task)
		deps := make([]*model.Task, 0, len(t.DependentTasks))
		for _, id := range t.DependentTasks {
			if dep, ok := m.tasks.Load(id); ok {
				deps = append(deps, dep.(*model.Task))
			}
		}
		return deps, nil
	}
	return nil, fmt.Errorf("task %s not found", taskID)
}

// GetDependencies gets all tasks that the given task depends on
func (m *DependencyManager) GetDependencies(taskID string) ([]*model.Task, error) {
	if task, ok := m.tasks.Load(taskID); ok {
		t := task.(*model.Task)
		deps := make([]*model.Task, 0, len(t.Dependencies))
		for _, id := range t.Dependencies {
			if dep, ok := m.tasks.Load(id); ok {
				deps = append(deps, dep.(*model.Task))
			}
		}
		return deps, nil
	}
	return nil, fmt.Errorf("task %s not found", taskID)
}
