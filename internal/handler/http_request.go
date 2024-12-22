package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/t77yq/nats-project/internal/model"
)

// HTTPRequestPayload represents the payload for HTTP request tasks
type HTTPRequestPayload struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Body    string           `json:"body"`
	Timeout time.Duration    `json:"timeout"`
}

// HTTPRequestHandler handles HTTP request tasks
type HTTPRequestHandler struct {
	logger     *zap.Logger
	httpClient *http.Client
}

// NewHTTPRequestHandler creates a new HTTP request handler
func NewHTTPRequestHandler(logger *zap.Logger) *HTTPRequestHandler {
	return &HTTPRequestHandler{
		logger: logger,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Execute performs the HTTP request
func (h *HTTPRequestHandler) Execute(ctx context.Context, task *model.Task) (*model.TaskResult, error) {
	var payload HTTPRequestPayload
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, payload.Method, payload.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	for key, value := range payload.Headers {
		req.Header.Add(key, value)
	}

	// Set client timeout if specified
	if payload.Timeout > 0 {
		h.httpClient.Timeout = payload.Timeout
	}

	// Execute request
	h.logger.Info("Executing HTTP request",
		zap.String("method", payload.Method),
		zap.String("url", payload.URL))

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Create result
	result := &model.TaskResult{
		TaskID:      task.ID,
		Status:      model.TaskStatusCompleted,
		CompletedAt: time.Now(),
		Result: body,
	}

	if resp.StatusCode >= 400 {
		result.Status = model.TaskStatusFailed
		result.Error = fmt.Sprintf("HTTP request failed with status: %d", resp.StatusCode)
	}

	return result, nil
}
