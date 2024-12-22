package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"go.uber.org/zap"
)

// LogEntry represents a log entry
type LogEntry struct {
	Timestamp time.Time     `json:"timestamp"`
	Level     string        `json:"level"`
	TaskID    string        `json:"task_id"`
	Message   string        `json:"message"`
	Data      interface{}   `json:"data,omitempty"`
}

// LogConfig defines configuration for log management
type LogConfig struct {
	LogDir        string        // Directory to store log files
	MaxFileSize   int64         // Maximum size of a log file in bytes
	MaxAge        time.Duration // Maximum age of log files
	FlushInterval time.Duration // Interval to flush logs to disk
}

// LogManager manages task execution logs
type LogManager struct {
	logger    *zap.Logger
	docker    *client.Client
	config    LogConfig
	mu        sync.RWMutex
	files     map[string]*os.File
	buffers   map[string][]LogEntry
}

// NewLogManager creates a new log manager
func NewLogManager(config LogConfig, logger *zap.Logger) (*LogManager, error) {
	// Create log directory if it doesn't exist
	if err := os.MkdirAll(config.LogDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	// Initialize Docker client with API version negotiation
	docker, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	return &LogManager{
		logger:   logger.Named("log-manager"),
		docker:   docker,
		config:   config,
		files:    make(map[string]*os.File),
		buffers:  make(map[string][]LogEntry),
	}, nil
}

// Start starts the log manager
func (lm *LogManager) Start(ctx context.Context) error {
	lm.logger.Info("Starting log manager")

	// Start log flushing
	go lm.flushLoop(ctx)

	// Start log rotation
	go lm.rotateLoop(ctx)

	return nil
}

// Stop stops the log manager
func (lm *LogManager) Stop() {
	lm.logger.Info("Stopping log manager")

	// Close all open files
	lm.mu.Lock()
	defer lm.mu.Unlock()

	for _, file := range lm.files {
		file.Close()
	}
}

// CollectLogs collects logs from a container
func (lm *LogManager) CollectLogs(ctx context.Context, containerID, taskID string) error {
	// Get container logs
	reader, err := lm.docker.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		return fmt.Errorf("failed to get container logs: %w", err)
	}
	defer reader.Close()

	// Create log file
	logFile, err := lm.createLogFile(taskID)
	if err != nil {
		return err
	}

	lm.mu.Lock()
	lm.files[taskID] = logFile
	lm.mu.Unlock()

	// Start collecting logs
	go lm.streamLogs(reader, taskID)

	return nil
}

// GetLogs retrieves logs for a task
func (lm *LogManager) GetLogs(taskID string, start, end time.Time) ([]LogEntry, error) {
	logFile := filepath.Join(lm.config.LogDir, fmt.Sprintf("%s.log", taskID))
	
	file, err := os.Open(logFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	var logs []LogEntry
	decoder := json.NewDecoder(file)

	for decoder.More() {
		var entry LogEntry
		if err := decoder.Decode(&entry); err != nil {
			return nil, fmt.Errorf("failed to decode log entry: %w", err)
		}

		if (entry.Timestamp.After(start) || entry.Timestamp.Equal(start)) &&
			(entry.Timestamp.Before(end) || entry.Timestamp.Equal(end)) {
			logs = append(logs, entry)
		}
	}

	return logs, nil
}

// AddLogEntry adds a log entry
func (lm *LogManager) AddLogEntry(taskID string, entry LogEntry) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	lm.buffers[taskID] = append(lm.buffers[taskID], entry)
	return nil
}

// createLogFile creates a new log file for a task
func (lm *LogManager) createLogFile(taskID string) (*os.File, error) {
	logFile := filepath.Join(lm.config.LogDir, fmt.Sprintf("%s.log", taskID))
	
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create log file: %w", err)
	}

	return file, nil
}

// streamLogs streams logs from a reader to a file
func (lm *LogManager) streamLogs(reader io.Reader, taskID string) {
	scanner := NewDockerLogScanner(reader)
	for scanner.Scan() {
		entry := LogEntry{
			Timestamp: time.Now(),
			TaskID:    taskID,
			Message:   scanner.Text(),
		}

		if err := lm.AddLogEntry(taskID, entry); err != nil {
			lm.logger.Error("Failed to add log entry",
				zap.String("task_id", taskID),
				zap.Error(err))
		}
	}

	if err := scanner.Err(); err != nil {
		lm.logger.Error("Failed to read container logs",
			zap.String("task_id", taskID),
			zap.Error(err))
	}
}

// flushLoop periodically flushes logs to disk
func (lm *LogManager) flushLoop(ctx context.Context) {
	ticker := time.NewTicker(lm.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lm.flushLogs()
		}
	}
}

// flushLogs flushes buffered logs to disk
func (lm *LogManager) flushLogs() {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	for taskID, entries := range lm.buffers {
		if len(entries) == 0 {
			continue
		}

		file, ok := lm.files[taskID]
		if !ok {
			var err error
			file, err = lm.createLogFile(taskID)
			if err != nil {
				lm.logger.Error("Failed to create log file",
					zap.String("task_id", taskID),
					zap.Error(err))
				continue
			}
			lm.files[taskID] = file
		}

		encoder := json.NewEncoder(file)
		for _, entry := range entries {
			if err := encoder.Encode(entry); err != nil {
				lm.logger.Error("Failed to write log entry",
					zap.String("task_id", taskID),
					zap.Error(err))
			}
		}

		// Clear buffer
		lm.buffers[taskID] = lm.buffers[taskID][:0]
	}
}

// rotateLoop periodically rotates log files
func (lm *LogManager) rotateLoop(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lm.rotateLogs()
		}
	}
}

// rotateLogs rotates log files based on size and age
func (lm *LogManager) rotateLogs() {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	now := time.Now()

	err := filepath.Walk(lm.config.LogDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Check file age
		if now.Sub(info.ModTime()) > lm.config.MaxAge {
			if err := os.Remove(path); err != nil {
				lm.logger.Error("Failed to remove old log file",
					zap.String("path", path),
					zap.Error(err))
			}
			return nil
		}

		// Check file size
		if info.Size() > lm.config.MaxFileSize {
			// Rotate file
			newPath := path + ".1"
			if err := os.Rename(path, newPath); err != nil {
				lm.logger.Error("Failed to rotate log file",
					zap.String("path", path),
					zap.Error(err))
			}
		}

		return nil
	})

	if err != nil {
		lm.logger.Error("Failed to rotate logs", zap.Error(err))
	}
}

// DockerLogScanner is a custom scanner for Docker logs
type DockerLogScanner struct {
	reader io.Reader
	buffer []byte
	err    error
}

// NewDockerLogScanner creates a new Docker log scanner
func NewDockerLogScanner(reader io.Reader) *DockerLogScanner {
	return &DockerLogScanner{
		reader: reader,
		buffer: make([]byte, 0, 4096),
	}
}

// Scan advances the scanner to the next log line
func (s *DockerLogScanner) Scan() bool {
	// Docker log format: [8]byte{STREAM_TYPE, 0, 0, 0, SIZE1, SIZE2, SIZE3, SIZE4}
	header := make([]byte, 8)
	_, err := io.ReadFull(s.reader, header)
	if err != nil {
		s.err = err
		return false
	}

	// Get message size from header
	size := int(header[4]) | int(header[5])<<8 | int(header[6])<<16 | int(header[7])<<24

	// Read message
	if cap(s.buffer) < size {
		s.buffer = make([]byte, size)
	}
	s.buffer = s.buffer[:size]

	_, err = io.ReadFull(s.reader, s.buffer)
	if err != nil {
		s.err = err
		return false
	}

	return true
}

// Text returns the current log line
func (s *DockerLogScanner) Text() string {
	return string(s.buffer)
}

// Err returns any error that occurred during scanning
func (s *DockerLogScanner) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}
