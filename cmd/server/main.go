package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/t77yq/nats-project/internal/executor"
	"github.com/t77yq/nats-project/internal/handler"
	"github.com/t77yq/nats-project/internal/model"
	"github.com/t77yq/nats-project/internal/scheduler"
	"github.com/t77yq/nats-project/internal/storage"
)

// ExampleTaskHandler implements a simple task handler
type ExampleTaskHandler struct {
	logger *zap.Logger
}

func (h *ExampleTaskHandler) Execute(ctx context.Context, task *model.Task) (*model.TaskResult, error) {
	h.logger.Info("Executing task",
		zap.String("task_id", task.ID),
		zap.String("task_name", task.Name))

	// Simulate some work
	time.Sleep(2 * time.Second)

	return &model.TaskResult{
		TaskID:      task.ID,
		Status:      model.TaskStatusCompleted,
		Result:      []byte("Task completed successfully"),
		CompletedAt: time.Now(),
	}, nil
}

func main() {
	// Initialize logger
	logger, err := zap.NewDevelopment()
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Sync()

	// Load configuration
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./config")
	if err := viper.ReadInConfig(); err != nil {
		logger.Fatal("Failed to read config file", zap.Error(err))
	}

	// Connect to NATS with more options
	opts := []nats.Option{
		nats.Name(viper.GetString("app.name")),
		nats.MaxReconnects(viper.GetInt("nats.max_reconnects")),
		nats.ReconnectWait(viper.GetDuration("nats.reconnect_wait")),
		nats.Timeout(viper.GetDuration("nats.connect_timeout")),
		nats.PingInterval(20 * time.Second),
		nats.MaxPingsOutstanding(5),
		nats.ReconnectBufSize(5 * 1024 * 1024), // 5MB
		nats.DrainTimeout(30 * time.Second),
		nats.ErrorHandler(func(nc *nats.Conn, sub *nats.Subscription, err error) {
			logger.Error("NATS connection error",
				zap.String("subject", sub.Subject),
				zap.Error(err))
		}),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			logger.Warn("NATS disconnected", zap.Error(err))
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			logger.Info("NATS reconnected",
				zap.String("url", nc.ConnectedUrl()))
		}),
	}

	// Connect with retry
	var nc *nats.Conn
	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		nc, err = nats.Connect(viper.GetString("nats.urls.0"), opts...)
		if err == nil {
			break
		}
		logger.Warn("Failed to connect to NATS, retrying...",
			zap.Int("attempt", i+1),
			zap.Error(err))
		time.Sleep(time.Second * time.Duration(i+1))
	}
	if err != nil {
		logger.Fatal("Failed to connect to NATS after retries", zap.Error(err))
	}
	defer nc.Close()

	logger.Info("Connected to NATS successfully",
		zap.String("url", nc.ConnectedUrl()))

	// Create JetStream context
	js, err := nc.JetStream()
	if err != nil {
		logger.Fatal("Failed to create JetStream context", zap.Error(err))
	}

	// Initialize scheduler
	taskScheduler, err := scheduler.NewNATSScheduler(js, logger)
	if err != nil {
		logger.Fatal("Failed to create scheduler", zap.Error(err))
	}

	// Create task history storage
	dbPath := "task_history.db"
	history, err := storage.NewSQLiteTaskHistory(logger, dbPath)
	if err != nil {
		logger.Fatal("Failed to create task history storage", zap.Error(err))
	}

	// Create executor config
	executorConfig := executor.ExecutorConfig{
		ID:         "executor-1",
		Name:       "Main Executor",
		Tags:       []string{"general"},
		MaxTasks:   10,
		MaxCPU:     80.0,               // 80% CPU limit
		MaxMemory:  1 << 30,            // 1GB memory limit
		LogDir:     "./logs/tasks",     // 使用相对路径
		MaxLogSize: 100 * 1024 * 1024,  // 100MB
		MaxLogAge:  7 * 24 * time.Hour, // 7 days
	}

	// Create executor
	taskExecutor, err := executor.NewExecutor(js, executorConfig, logger, history)
	if err != nil {
		logger.Fatal("Failed to create executor", zap.Error(err))
	}

	// Initialize and register task handlers
	httpHandler := handler.NewHTTPRequestHandler(logger)
	shellHandler := handler.NewShellCommandHandler(logger)
	dataHandler := handler.NewDataProcessingHandler(logger)
	fileHandler := handler.NewFileOperationHandler(logger, "/tmp/taskhandler")
	notificationHandler := handler.NewNotificationHandler(logger, handler.NotificationConfig{
		Email: struct {
			Host     string
			Port     int
			Username string
			Password string
			From     string
		}{
			Host:     "smtp.example.com",
			Port:     587,
			Username: "user@example.com",
			Password: "password",
			From:     "noreply@example.com",
		},
	})
	exampleHandler := &ExampleTaskHandler{logger: logger}

	taskExecutor.RegisterHandler("http_request", httpHandler)
	taskExecutor.RegisterHandler("shell_command", shellHandler)
	taskExecutor.RegisterHandler("data_processing", dataHandler)
	taskExecutor.RegisterHandler("file_operation", fileHandler)
	taskExecutor.RegisterHandler("notification", notificationHandler)
	taskExecutor.RegisterHandler("example", exampleHandler)

	// Setup signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		logger.Info("Received shutdown signal", zap.String("signal", sig.String()))
		cancel()
	}()

	// Submit example tasks
	exampleTasks := []struct {
		name    string
		payload interface{}
	}{
		{
			name: "http_request",
			payload: handler.HTTPRequestPayload{
				URL:     "https://api.github.com",
				Method:  "GET",
				Headers: map[string]string{"Accept": "application/json"},
				Timeout: 10 * time.Second,
			},
		},
		{
			name: "shell_command",
			payload: handler.ShellCommandPayload{
				Command: "echo",
				Args:    []string{"Hello, World!"},
				Timeout: 5 * time.Second,
			},
		},
		{
			name: "data_processing",
			payload: handler.DataProcessingPayload{
				InputData: []int{1, 2, 3, 4, 5},
				Operation: "transform",
				Parameters: map[string]interface{}{
					"multiply_by": 2,
				},
			},
		},
		{
			name: "file_operation",
			payload: handler.FileOperationPayload{
				Operation:  handler.FileOperationWrite,
				SourcePath: "test.txt",
				Content:    []byte("Hello, World!"),
			},
		},
		{
			name: "notification",
			payload: handler.NotificationPayload{
				Type:       handler.NotificationEmail,
				Template:   "Hello, this is a test notification",
				Recipients: []string{"user@example.com"},
				Options: map[string]string{
					"priority": "high",
				},
			},
		},
		{
			name:    "example",
			payload: struct{}{},
		},
	}

	for i, task := range exampleTasks {
		payload, err := json.Marshal(task.payload)
		if err != nil {
			logger.Error("Failed to marshal task payload", zap.Error(err))
			continue
		}

		exampleTask := &model.Task{
			ID:          fmt.Sprintf("task-%d", i+1),
			Name:        task.name,
			Description: fmt.Sprintf("Example %s task", task.name),
			Priority:    model.TaskPriorityNormal,
			Payload:     payload,
			CreatedAt:   time.Now(),
			Status:      model.TaskStatusPending,
		}

		if err := taskScheduler.SubmitTask(ctx, exampleTask); err != nil {
			logger.Error("Failed to submit task",
				zap.String("task_name", task.name),
				zap.Error(err))
		}
	}

	// Start monitoring running tasks and cleanup old history
	go func() {
		taskTicker := time.NewTicker(5 * time.Second)
		cleanupTicker := time.NewTicker(24 * time.Hour)
		defer taskTicker.Stop()
		defer cleanupTicker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-taskTicker.C:
				runningTasks := taskExecutor.GetRunningTasks()
				logger.Info("Current running tasks", zap.Int("count", len(runningTasks)))

				// Get recent task history
				histories, err := taskExecutor.GetTaskHistory(ctx, nil, 0, 10)
				if err != nil {
					logger.Error("Failed to get task history", zap.Error(err))
					continue
				}

				for _, history := range histories {
					logger.Info("Recent task execution",
						zap.String("task_id", history.TaskID),
						zap.String("name", history.Name),
						zap.String("status", string(history.Status)),
						zap.Duration("duration", history.Duration))
				}
			case <-cleanupTicker.C:
				// Cleanup task history older than 30 days
				cutoff := time.Now().AddDate(0, 0, -30)
				if err := taskExecutor.CleanupOldHistory(ctx, cutoff); err != nil {
					logger.Error("Failed to cleanup old task history", zap.Error(err))
				}
			}
		}
	}()

	// Wait for shutdown signal
	<-ctx.Done()

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// Wait for running tasks to complete
	runningTasks := taskExecutor.GetRunningTasks()
	if len(runningTasks) > 0 {
		logger.Info("Waiting for running tasks to complete",
			zap.Int("count", len(runningTasks)))

		select {
		case <-shutdownCtx.Done():
			logger.Warn("Shutdown timeout reached, some tasks may not have completed")
		case <-time.After(5 * time.Second):
			logger.Info("All tasks completed successfully")
		}
	}

	logger.Info("Server shutting down gracefully")
}
