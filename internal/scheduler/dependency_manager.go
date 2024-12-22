package scheduler

import (
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/t77yq/nats-project/internal/model"
)

// DependencyManager manages task dependencies and tracks their status
type DependencyManager struct {
	logger *zap.Logger
	mu     sync.RWMutex
	tasks  map[string]*model.Task // Map of task ID to task
	graph  map[string][]string    // Adjacency list representation of task dependencies
}

// NewDependencyManager creates a new dependency manager
func NewDependencyManager(logger *zap.Logger) *DependencyManager {
	return &DependencyManager{
		logger: logger.Named("dependency-manager"),
		tasks:  make(map[string]*model.Task),
		graph:  make(map[string][]string),
	}
}

// AddTask adds a new task to the dependency manager
func (m *DependencyManager) AddTask(task *model.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for circular dependencies
	if err := m.checkCircularDependencies(task.ID, task.Dependencies); err != nil {
		return err
	}

	// Add task to the map
	m.tasks[task.ID] = task

	// Add dependencies to the graph
	m.graph[task.ID] = task.Dependencies

	// Add this task as a dependent to its dependencies
	for _, depID := range task.Dependencies {
		if dep, exists := m.tasks[depID]; exists {
			dep.DependentTasks = append(dep.DependentTasks, task.ID)
		}
	}

	// Update task status based on dependencies
	m.updateTaskStatus(task)

	return nil
}

// RemoveTask removes a task and its dependencies
func (m *DependencyManager) RemoveTask(taskID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Remove task from dependencies of other tasks
	if task, exists := m.tasks[taskID]; exists {
		for _, depID := range task.Dependencies {
			if dep, ok := m.tasks[depID]; ok {
				// Remove taskID from dependent_tasks
				for i, id := range dep.DependentTasks {
					if id == taskID {
						dep.DependentTasks = append(dep.DependentTasks[:i], dep.DependentTasks[i+1:]...)
						break
					}
				}
			}
		}
	}

	// Remove task from the maps
	delete(m.tasks, taskID)
	delete(m.graph, taskID)
}

// UpdateTaskStatus updates the status of a task and its dependents
func (m *DependencyManager) UpdateTaskStatus(taskID string, status model.TaskStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if task, exists := m.tasks[taskID]; exists {
		task.Status = status

		// If task is completed, update dependent tasks
		if status == model.TaskStatusComplete {
			for _, depID := range task.DependentTasks {
				if dep, ok := m.tasks[depID]; ok {
					m.updateTaskStatus(dep)
				}
			}
		}
	}
}

// GetReadyTasks returns a list of tasks that are ready to be executed
func (m *DependencyManager) GetReadyTasks() []*model.Task {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var readyTasks []*model.Task
	for _, task := range m.tasks {
		if task.Status == model.TaskStatusReady {
			readyTasks = append(readyTasks, task)
		}
	}
	return readyTasks
}

// GetBlockedTasks returns a list of tasks that are blocked by dependencies
func (m *DependencyManager) GetBlockedTasks() []*model.Task {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var blockedTasks []*model.Task
	for _, task := range m.tasks {
		if task.Status == model.TaskStatusBlocked {
			blockedTasks = append(blockedTasks, task)
		}
	}
	return blockedTasks
}

// checkCircularDependencies checks if adding the given dependencies would create a cycle
func (m *DependencyManager) checkCircularDependencies(taskID string, deps []string) error {
	visited := make(map[string]bool)
	path := make(map[string]bool)

	var visit func(string) error
	visit = func(current string) error {
		if path[current] {
			return fmt.Errorf("circular dependency detected: task %s", current)
		}
		if visited[current] {
			return nil
		}

		visited[current] = true
		path[current] = true

		// Check existing dependencies in the graph
		for _, dep := range m.graph[current] {
			if err := visit(dep); err != nil {
				return err
			}
		}

		// Check new dependencies if we're at the target task
		if current == taskID {
			for _, dep := range deps {
				if err := visit(dep); err != nil {
					return err
				}
			}
		}

		path[current] = false
		return nil
	}

	return visit(taskID)
}

// updateTaskStatus updates a task's status based on its dependencies
func (m *DependencyManager) updateTaskStatus(task *model.Task) {
	// Check if all dependencies are completed
	allDepsCompleted := true
	for _, depID := range task.Dependencies {
		if dep, exists := m.tasks[depID]; exists {
			if dep.Status != model.TaskStatusComplete {
				allDepsCompleted = false
				break
			}
		}
	}

	// Update task status
	if len(task.Dependencies) == 0 {
		if task.Status == model.TaskStatusPending {
			task.Status = model.TaskStatusReady
		}
	} else if allDepsCompleted {
		task.Status = model.TaskStatusReady
	} else {
		task.Status = model.TaskStatusBlocked
	}
}
