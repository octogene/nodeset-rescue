//go:build ns

package router

import (
	"database/sql"
	"fmt"
	"go.uber.org/zap"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type UsageTracker interface {
	RecordUsage(indices []string) error
	ViewUsage(from time.Time, to time.Time) (map[string]time.Duration, error) // [ validator_pubkey ] -> [ duration ]
	Close()
}

type SQLiteUsageTracker struct {
	Database  *sql.DB
	Logger    *zap.Logger
	Precision time.Duration
}

func NewSQLiteUsageTracker(logger *zap.Logger) UsageTracker {
	db, err := sql.Open("sqlite3", "file:nodeset-usage.db?cache=shared")
	if err != nil {
		logger.Fatal("Failed to open SQLite database", zap.Error(err))
	}

	db.SetMaxOpenConns(1)

	tracker := &SQLiteUsageTracker{
		Database:  db,
		Logger:    logger,
		Precision: 5 * time.Minute,
	}

	if err := tracker.initSchema(); err != nil {
		logger.Fatal("Failed to initialize database schema", zap.Error(err))
	}

	return tracker
}

func (tracker *SQLiteUsageTracker) initSchema() error {
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS validator_usage (
		timestamp DATETIME NOT NULL,
		validator_index TEXT NOT NULL,
		PRIMARY KEY (timestamp, validator_index)
	);
	
	CREATE INDEX IF NOT EXISTS idx_timestamp ON validator_usage(timestamp);
	CREATE INDEX IF NOT EXISTS idx_validator ON validator_usage(validator_index);
	`

	_, err := tracker.Database.Exec(createTableSQL)
	return err
}

func (tracker *SQLiteUsageTracker) RecordUsage(indexes []string) error {
	timestampUnix := time.Now().Truncate(tracker.Precision).Unix()

	tx, err := tracker.Database.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("INSERT OR IGNORE INTO validator_usage (timestamp, validator_index) VALUES (datetime(?, 'unixepoch'), ?)")
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, index := range indexes {
		_, err := stmt.Exec(timestampUnix, index)
		if err != nil {
			tracker.Logger.Error("Failed to store index usage",
				zap.String("index", index),
				zap.Int64("timestamp_unix", timestampUnix),
				zap.Error(err))
			return fmt.Errorf("failed to insert usage for validator %s at %d: %w", index, timestampUnix, err)
		}

		tracker.Logger.Debug("Recorded index usage",
			zap.String("index", index),
			zap.Int64("quantized_timestamp_unix", timestampUnix),
			zap.Duration("precision", tracker.Precision))
	}

	return tx.Commit()
}

func (tracker *SQLiteUsageTracker) ViewUsage(from time.Time, to time.Time) (map[string]time.Duration, error) {
	result := make(map[string]time.Duration)

	fromUnix := from.Truncate(tracker.Precision).Unix()
	toUnix := to.Truncate(tracker.Precision).Unix()

	query := `
	SELECT validator_index, COUNT(*) as usage_count
	FROM validator_usage 
	WHERE timestamp >= datetime(?, 'unixepoch') AND timestamp <= datetime(?, 'unixepoch')
	GROUP BY validator_index
	`

	rows, err := tracker.Database.Query(query, fromUnix, toUnix)
	if err != nil {
		return nil, fmt.Errorf("failed to query usage data: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var validator string
		var count int

		if err := rows.Scan(&validator, &count); err != nil {
			tracker.Logger.Error("Failed to scan row", zap.Error(err))
			continue
		}

		var duration time.Duration
		for i := 0; i < count; i++ {
			duration += tracker.Precision
		}
		result[validator] = duration

		tracker.Logger.Debug("Found usage record",
			zap.String("validator", validator),
			zap.Int("count", count),
			zap.Duration("total_duration", time.Duration(count)*tracker.Precision))
	}

	return result, rows.Err()
}

func (tracker *SQLiteUsageTracker) Close() {
	if err := tracker.Database.Close(); err != nil {
		tracker.Logger.Error("Failed to close SQLite database", zap.Error(err))
	}
}
