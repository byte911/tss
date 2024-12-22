package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/t77yq/nats-project/internal/model"
)

// ShellCommandPayload represents the payload for shell command tasks
type ShellCommandPayload struct {
	Command    string            `json:"command"`
	Args       []string          `json:"args"`
	Env        map[string]string `json:"env"`
	WorkingDir string            `json:"working_dir"`
	Timeout    time.Duration     `json:"timeout"`
}

// ShellCommandHandler handles shell command execution tasks
type ShellCommandHandler struct {
	logger *zap.Logger
}

// NewShellCommandHandler creates a new shell command handler
func NewShellCommandHandler(logger *zap.Logger) *ShellCommandHandler {
	return &ShellCommandHandler{
		logger: logger,
	}
}

// Execute runs the shell command
func (h *ShellCommandHandler) Execute(ctx context.Context, task *model.Task) (*model.TaskResult, error) {
	var payload ShellCommandPayload
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	// Create command context with timeout
	cmdCtx := ctx
	if payload.Timeout > 0 {
		var cancel context.CancelFunc
		cmdCtx, cancel = context.WithTimeout(ctx, payload.Timeout)
		defer cancel()
	}

	// Create command
	cmd := exec.CommandContext(cmdCtx, payload.Command, payload.Args...)

	// Set working directory if specified
	if payload.WorkingDir != "" {
		cmd.Dir = payload.WorkingDir
	}

	// Set environment variables
	if len(payload.Env) > 0 {
		env := make([]string, 0, len(payload.Env))
		for k, v := range payload.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		cmd.Env = append(cmd.Env, env...)
	}

	h.logger.Info("Executing shell command",
		zap.String("command", payload.Command),
		zap.Strings("args", payload.Args))

	// Execute command and capture output
	output, err := cmd.CombinedOutput()

	// Create result
	result := &model.TaskResult{
		TaskID:      task.ID,
		CompletedAt: time.Now(),
		Result:      output,
	}

	if err != nil {
		if cmdCtx.Err() == context.DeadlineExceeded {
			result.Status = model.TaskStatusFailed
			result.Error = "command execution timed out"
		} else {
			result.Status = model.TaskStatusFailed
			result.Error = strings.TrimSpace(string(output))
		}
	} else {
		result.Status = model.TaskStatusCompleted
	}

	return result, nil
}
