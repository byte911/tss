package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"

	"github.com/t77yq/nats-project/internal/model"
)

// FileOperationType defines the type of file operation
type FileOperationType string

const (
	FileOperationRead    FileOperationType = "read"
	FileOperationWrite   FileOperationType = "write"
	FileOperationDelete  FileOperationType = "delete"
	FileOperationMove    FileOperationType = "move"
	FileOperationCopy    FileOperationType = "copy"
	FileOperationArchive FileOperationType = "archive"
)

// FileOperationPayload represents the payload for file operation tasks
type FileOperationPayload struct {
	Operation   FileOperationType `json:"operation"`
	SourcePath  string           `json:"source_path"`
	TargetPath  string           `json:"target_path,omitempty"`
	Content     []byte           `json:"content,omitempty"`
	Permissions os.FileMode      `json:"permissions,omitempty"`
}

// FileOperationHandler handles file operations
type FileOperationHandler struct {
	logger *zap.Logger
	// Base directory for all file operations
	baseDir string
}

// NewFileOperationHandler creates a new file operation handler
func NewFileOperationHandler(logger *zap.Logger, baseDir string) *FileOperationHandler {
	return &FileOperationHandler{
		logger:  logger,
		baseDir: baseDir,
	}
}

// Execute performs the file operation
func (h *FileOperationHandler) Execute(ctx context.Context, task *model.Task) (*model.TaskResult, error) {
	var payload FileOperationPayload
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	// Validate and sanitize paths
	sourcePath := filepath.Clean(filepath.Join(h.baseDir, payload.SourcePath))
	if !filepath.HasPrefix(sourcePath, h.baseDir) {
		return nil, fmt.Errorf("source path must be within base directory")
	}

	var targetPath string
	if payload.TargetPath != "" {
		targetPath = filepath.Clean(filepath.Join(h.baseDir, payload.TargetPath))
		if !filepath.HasPrefix(targetPath, h.baseDir) {
			return nil, fmt.Errorf("target path must be within base directory")
		}
	}

	var result []byte
	var err error

	h.logger.Info("Executing file operation",
		zap.String("operation", string(payload.Operation)),
		zap.String("source", sourcePath))

	switch payload.Operation {
	case FileOperationRead:
		result, err = h.readFile(sourcePath)
	case FileOperationWrite:
		err = h.writeFile(sourcePath, payload.Content, payload.Permissions)
	case FileOperationDelete:
		err = h.deleteFile(sourcePath)
	case FileOperationMove:
		err = h.moveFile(sourcePath, targetPath)
	case FileOperationCopy:
		err = h.copyFile(sourcePath, targetPath)
	case FileOperationArchive:
		result, err = h.archiveFile(sourcePath, targetPath)
	default:
		return nil, fmt.Errorf("unsupported operation: %s", payload.Operation)
	}

	if err != nil {
		return &model.TaskResult{
			TaskID:      task.ID,
			Status:      model.TaskStatusFailed,
			Error:       err.Error(),
			CompletedAt: time.Now(),
		}, nil
	}

	return &model.TaskResult{
		TaskID:      task.ID,
		Status:      model.TaskStatusCompleted,
		Result:      result,
		CompletedAt: time.Now(),
	}, nil
}

func (h *FileOperationHandler) readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (h *FileOperationHandler) writeFile(path string, content []byte, perm os.FileMode) error {
	if perm == 0 {
		perm = 0644
	}
	return os.WriteFile(path, content, perm)
}

func (h *FileOperationHandler) deleteFile(path string) error {
	return os.Remove(path)
}

func (h *FileOperationHandler) moveFile(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}
	return os.Rename(source, target)
}

func (h *FileOperationHandler) copyFile(source, target string) error {
	sourceFile, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer sourceFile.Close()

	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	targetFile, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("failed to create target file: %w", err)
	}
	defer targetFile.Close()

	if _, err = io.Copy(targetFile, sourceFile); err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	sourceInfo, err := sourceFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to get source file info: %w", err)
	}

	return os.Chmod(target, sourceInfo.Mode())
}

func (h *FileOperationHandler) archiveFile(source, target string) ([]byte, error) {
	// TODO: Implement file archiving (zip, tar, etc.)
	return nil, fmt.Errorf("archive operation not implemented")
}
