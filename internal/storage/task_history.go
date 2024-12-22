package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"go.uber.org/zap"

	"github.com/t77yq/nats-project/internal/model"
)

// TaskHistory represents a historical task execution record
type TaskHistory struct {
	ID          string           `json:"id"`
	TaskID      string           `json:"task_id"`
	Name        string           `json:"name"`
	Status      model.TaskStatus `json:"status"`
	Payload     json.RawMessage  `json:"payload,omitempty"`
	Result      json.RawMessage  `json:"result,omitempty"`
	Error       string           `json:"error,omitempty"`
	StartedAt   time.Time        `json:"started_at"`
	CompletedAt *time.Time       `json:"completed_at,omitempty"`
	Duration    time.Duration    `json:"duration,omitempty"`
	Metadata    json.RawMessage  `json:"metadata,omitempty"`
}

// TaskHistoryStorage defines the interface for task history storage
type TaskHistoryStorage interface {
	// Store stores a task execution record
	Store(ctx context.Context, history *TaskHistory) error

	// Update updates an existing task execution record
	Update(ctx context.Context, history *TaskHistory) error

	// Get retrieves a task execution record by ID
	Get(ctx context.Context, id string) (*TaskHistory, error)

	// List retrieves task execution records with pagination and filters
	List(ctx context.Context, filters map[string]interface{}, offset, limit int) ([]*TaskHistory, error)

	// Count returns the total number of records matching the filters
	Count(ctx context.Context, filters map[string]interface{}) (int, error)

	// DeleteBefore deletes records older than the specified time
	DeleteBefore(ctx context.Context, before time.Time) error
}

// SQLiteTaskHistory implements TaskHistoryStorage using SQLite
type SQLiteTaskHistory struct {
	logger *zap.Logger
	db     *sql.DB
}

// NewSQLiteTaskHistory creates a new SQLite-based task history storage
func NewSQLiteTaskHistory(logger *zap.Logger, dbPath string) (*SQLiteTaskHistory, error) {
	// Remove existing database file if it exists
	if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to remove old database: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	storage := &SQLiteTaskHistory{
		logger: logger,
		db:     db,
	}

	if err := storage.initialize(); err != nil {
		db.Close()
		return nil, err
	}

	return storage, nil
}

// initialize creates the necessary tables if they don't exist
func (s *SQLiteTaskHistory) initialize() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS task_history (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL,
			name TEXT NOT NULL,
			status TEXT NOT NULL,
			payload TEXT,
			result TEXT,
			error TEXT,
			started_at DATETIME NOT NULL,
			completed_at DATETIME,
			duration INTEGER,
			metadata TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_task_history_task_id ON task_history(task_id);
		CREATE INDEX IF NOT EXISTS idx_task_history_name ON task_history(name);
		CREATE INDEX IF NOT EXISTS idx_task_history_status ON task_history(status);
		CREATE INDEX IF NOT EXISTS idx_task_history_started_at ON task_history(started_at);
	`)
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	return nil
}

// Store implements TaskHistoryStorage.Store
func (s *SQLiteTaskHistory) Store(ctx context.Context, history *TaskHistory) error {
	var payloadStr string
	if len(history.Payload) > 0 {
		payloadStr = string(history.Payload)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO task_history (
			id, task_id, name, status, payload, started_at
		) VALUES (?, ?, ?, ?, ?, ?)`,
		history.ID,
		history.TaskID,
		history.Name,
		history.Status,
		payloadStr,
		history.StartedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to store task history: %w", err)
	}
	return nil
}

// Update implements TaskHistoryStorage.Update
func (s *SQLiteTaskHistory) Update(ctx context.Context, history *TaskHistory) error {
	var resultStr, metadataStr string
	if len(history.Result) > 0 {
		resultStr = string(history.Result)
	}
	if len(history.Metadata) > 0 {
		metadataStr = string(history.Metadata)
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE task_history SET
			status = ?,
			result = ?,
			error = ?,
			completed_at = ?,
			duration = ?,
			metadata = ?
		WHERE id = ?`,
		history.Status,
		sql.NullString{String: resultStr, Valid: len(resultStr) > 0},
		sql.NullString{String: history.Error, Valid: history.Error != ""},
		sql.NullTime{Time: *history.CompletedAt, Valid: history.CompletedAt != nil},
		sql.NullInt64{Int64: int64(history.Duration), Valid: history.Duration != 0},
		sql.NullString{String: metadataStr, Valid: len(metadataStr) > 0},
		history.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update task history: %w", err)
	}
	return nil
}

// Get implements TaskHistoryStorage.Get
func (s *SQLiteTaskHistory) Get(ctx context.Context, id string) (*TaskHistory, error) {
	var history TaskHistory
	var payload, result, metadata, errorStr sql.NullString
	var completedAt sql.NullTime
	var durationNanos sql.NullInt64

	err := s.db.QueryRowContext(ctx, `
		SELECT 
			id, task_id, name, status, payload, result, error,
			started_at, completed_at, duration, metadata
		FROM task_history
		WHERE id = ?`, id).Scan(
		&history.ID,
		&history.TaskID,
		&history.Name,
		&history.Status,
		&payload,
		&result,
		&errorStr,
		&history.StartedAt,
		&completedAt,
		&durationNanos,
		&metadata,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to scan task history: %w", err)
	}

	if payload.Valid && payload.String != "" {
		history.Payload = json.RawMessage(payload.String)
	}
	if result.Valid && result.String != "" {
		history.Result = json.RawMessage(result.String)
	}
	if errorStr.Valid {
		history.Error = errorStr.String
	}
	if completedAt.Valid {
		history.CompletedAt = &completedAt.Time
	}
	if durationNanos.Valid {
		history.Duration = time.Duration(durationNanos.Int64)
	}
	if metadata.Valid && metadata.String != "" {
		history.Metadata = json.RawMessage(metadata.String)
	}

	return &history, nil
}

// List implements TaskHistoryStorage.List
func (s *SQLiteTaskHistory) List(ctx context.Context, filters map[string]interface{}, offset, limit int) ([]*TaskHistory, error) {
	query := "SELECT id, task_id, name, status, payload, result, error, started_at, completed_at, duration, metadata FROM task_history"
	args := make([]interface{}, 0)

	if len(filters) > 0 {
		query += " WHERE"
		first := true
		for key, value := range filters {
			if !first {
				query += " AND"
			}
			query += fmt.Sprintf(" %s = ?", key)
			args = append(args, value)
			first = false
		}
	}

	query += " ORDER BY started_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list task history: %w", err)
	}
	defer rows.Close()

	var histories []*TaskHistory
	for rows.Next() {
		history := &TaskHistory{}
		var payload, result, metadata, errorStr sql.NullString
		var completedAt sql.NullTime
		var durationNanos sql.NullInt64

		err := rows.Scan(
			&history.ID,
			&history.TaskID,
			&history.Name,
			&history.Status,
			&payload,
			&result,
			&errorStr,
			&history.StartedAt,
			&completedAt,
			&durationNanos,
			&metadata,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan task history: %w", err)
		}

		if payload.Valid && payload.String != "" {
			history.Payload = json.RawMessage(payload.String)
		}
		if result.Valid && result.String != "" {
			history.Result = json.RawMessage(result.String)
		}
		if errorStr.Valid {
			history.Error = errorStr.String
		}
		if completedAt.Valid {
			history.CompletedAt = &completedAt.Time
		}
		if durationNanos.Valid {
			history.Duration = time.Duration(durationNanos.Int64)
		}
		if metadata.Valid && metadata.String != "" {
			history.Metadata = json.RawMessage(metadata.String)
		}

		histories = append(histories, history)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during row iteration: %w", err)
	}

	return histories, nil
}

// Count implements TaskHistoryStorage.Count
func (s *SQLiteTaskHistory) Count(ctx context.Context, filters map[string]interface{}) (int, error) {
	query := "SELECT COUNT(*) FROM task_history"
	args := make([]interface{}, 0)

	if len(filters) > 0 {
		query += " WHERE"
		first := true
		for key, value := range filters {
			if !first {
				query += " AND"
			}
			query += fmt.Sprintf(" %s = ?", key)
			args = append(args, value)
			first = false
		}
	}

	var count int
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count task history: %w", err)
	}
	return count, nil
}

// DeleteBefore implements TaskHistoryStorage.DeleteBefore
func (s *SQLiteTaskHistory) DeleteBefore(ctx context.Context, before time.Time) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM task_history WHERE started_at < ?", before)
	if err != nil {
		return fmt.Errorf("failed to delete task history: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}

	s.logger.Info("Deleted old task history records",
		zap.Time("before", before),
		zap.Int64("deleted", affected))

	return nil
}

// Close closes the database connection
func (s *SQLiteTaskHistory) Close() error {
	return s.db.Close()
}
