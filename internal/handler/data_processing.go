package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/t77yq/nats-project/internal/model"
)

// DataProcessingPayload represents the payload for data processing tasks
type DataProcessingPayload struct {
	InputData    interface{} `json:"input_data"`
	Operation    string      `json:"operation"`
	Parameters   interface{} `json:"parameters"`
}

// DataProcessingHandler handles data processing tasks
type DataProcessingHandler struct {
	logger *zap.Logger
	processors map[string]DataProcessor
}

// DataProcessor defines the interface for data processing operations
type DataProcessor interface {
	Process(ctx context.Context, data interface{}, params interface{}) (interface{}, error)
}

// NewDataProcessingHandler creates a new data processing handler
func NewDataProcessingHandler(logger *zap.Logger) *DataProcessingHandler {
	h := &DataProcessingHandler{
		logger:     logger,
		processors: make(map[string]DataProcessor),
	}

	// Register default processors
	h.RegisterProcessor("filter", &FilterProcessor{})
	h.RegisterProcessor("transform", &TransformProcessor{})
	h.RegisterProcessor("aggregate", &AggregateProcessor{})

	return h
}

// RegisterProcessor registers a new data processor
func (h *DataProcessingHandler) RegisterProcessor(operation string, processor DataProcessor) {
	h.processors[operation] = processor
}

// Execute performs the data processing task
func (h *DataProcessingHandler) Execute(ctx context.Context, task *model.Task) (*model.TaskResult, error) {
	var payload DataProcessingPayload
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	processor, ok := h.processors[payload.Operation]
	if !ok {
		return nil, fmt.Errorf("unknown operation: %s", payload.Operation)
	}

	h.logger.Info("Processing data",
		zap.String("operation", payload.Operation))

	// Process data
	result, err := processor.Process(ctx, payload.InputData, payload.Parameters)
	if err != nil {
		return &model.TaskResult{
			TaskID:      task.ID,
			Status:      model.TaskStatusFailed,
			Error:       err.Error(),
			CompletedAt: time.Now(),
		}, nil
	}

	// Marshal result
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	return &model.TaskResult{
		TaskID:      task.ID,
		Status:      model.TaskStatusCompleted,
		Result:      resultBytes,
		CompletedAt: time.Now(),
	}, nil
}

// FilterProcessor implements filtering operations
type FilterProcessor struct{}

func (p *FilterProcessor) Process(ctx context.Context, data interface{}, params interface{}) (interface{}, error) {
	// Implement filtering logic
	return data, nil
}

// TransformProcessor implements data transformation operations
type TransformProcessor struct{}

func (p *TransformProcessor) Process(ctx context.Context, data interface{}, params interface{}) (interface{}, error) {
	// Implement transformation logic
	return data, nil
}

// AggregateProcessor implements data aggregation operations
type AggregateProcessor struct{}

func (p *AggregateProcessor) Process(ctx context.Context, data interface{}, params interface{}) (interface{}, error) {
	// Implement aggregation logic
	return data, nil
}
