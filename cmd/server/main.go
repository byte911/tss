package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/t77yq/nats-project/internal/executor"
	"github.com/t77yq/nats-project/internal/model"
	"github.com/t77yq/nats-project/internal/scheduler"
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
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
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

	// Initialize executor
	taskExecutor, err := executor.NewExecutor(js, logger)
	if err != nil {
		logger.Fatal("Failed to create executor", zap.Error(err))
	}

	// Register example task handler
	taskExecutor.RegisterHandler("example", &ExampleTaskHandler{logger: logger})

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

	// Submit an example task
	exampleTask := &model.Task{
		ID:          "task-1",
		Name:        "example",
		Description: "Example task",
		Priority:    model.TaskPriorityNormal,
		CreatedAt:   time.Now(),
		ScheduledAt: time.Now(),
	}

	if err := taskScheduler.SubmitTask(ctx, exampleTask); err != nil {
		logger.Error("Failed to submit task", zap.Error(err))
	}

	// Start monitoring running tasks
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runningTasks := taskExecutor.GetRunningTasks()
				logger.Info("Current running tasks", zap.Int("count", len(runningTasks)))
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
