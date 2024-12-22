package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/t77yq/nats-project/internal/model"
)

// DBOperationType defines the type of database operation
type DBOperationType string

const (
	DBOperationQuery DBOperationType = "query"
	DBOperationExec  DBOperationType = "exec"
)

// DBOperationPayload represents the payload for database operation tasks
type DBOperationPayload struct {
	Operation DBOperationType    `json:"operation"`
	Query     string            `json:"query"`
	Args      []interface{}     `json:"args"`
	Options   map[string]string `json:"options"`
}

// DatabaseOperationHandler handles database operations
type DatabaseOperationHandler struct {
	logger *zap.Logger
	db     *sql.DB
}

// NewDatabaseOperationHandler creates a new database operation handler
func NewDatabaseOperationHandler(logger *zap.Logger, db *sql.DB) *DatabaseOperationHandler {
	return &DatabaseOperationHandler{
		logger: logger,
		db:     db,
	}
}

// Execute performs the database operation
func (h *DatabaseOperationHandler) Execute(ctx context.Context, task *model.Task) (*model.TaskResult, error) {
	var payload DBOperationPayload
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	h.logger.Info("Executing database operation",
		zap.String("operation", string(payload.Operation)),
		zap.String("query", payload.Query))

	var result interface{}
	var err error

	switch payload.Operation {
	case DBOperationQuery:
		result, err = h.executeQuery(ctx, payload.Query, payload.Args...)
	case DBOperationExec:
		result, err = h.executeExec(ctx, payload.Query, payload.Args...)
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

	// Marshal the result
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

func (h *DatabaseOperationHandler) executeQuery(ctx context.Context, query string, args ...interface{}) (interface{}, error) {
	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	var results []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		for i := range values {
			values[i] = new(interface{})
		}

		if err := rows.Scan(values...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		row := make(map[string]interface{})
		for i, column := range columns {
			row[column] = *(values[i].(*interface{}))
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during row iteration: %w", err)
	}

	return results, nil
}

func (h *DatabaseOperationHandler) executeExec(ctx context.Context, query string, args ...interface{}) (interface{}, error) {
	result, err := h.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute statement: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get affected rows: %w", err)
	}

	lastID, err := result.LastInsertId()
	if err != nil {
		// Not all databases support LastInsertId
		lastID = 0
	}

	return map[string]int64{
		"affected_rows": affected,
		"last_insert_id": lastID,
	}, nil
}
