//go:build ns

package router

import (
	"database/sql"
	"fmt"
	"github.com/Rocket-Rescue-Node/rescue-proxy/test"
	"math/rand"
	"testing"
	"testing/synctest"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/rocket-pool/rocketpool-go/types"
	"go.uber.org/zap/zaptest"
)

func TestSQLiteUsageTracker(t *testing.T) {
	tracker, cleanup, err := setupSQLiteTestDatabase(t, 5*time.Minute)
	if err != nil {
		t.Fatal("Failed to set up test database:", err)
	}
	defer cleanup()

	// Test data
	random := rand.New(rand.NewSource(time.Now().UnixNano()))
	validators := make([]string, 10)
	for i := 0; i < 10; i++ {
		validators[i] = test.RandPubkey(random).Hex()
	}

	synctest.Run(func() {
		// Record usage for both validators
		err = tracker.RecordUsage(validators[0:5])
		if err != nil {
			t.Fatal("Failed to record usage for validator1:", err)
		}

		// Wait for next time bucket and record validator1 again
		time.Sleep(6 * time.Minute)
		err = tracker.RecordUsage(validators)
		if err != nil {
			t.Fatal("Failed to record second usage for validator1:", err)
		}

		// View usage for a wide time range
		now := time.Now()
		from := now.Add(-2 * time.Hour)
		to := now.Add(2 * time.Hour)

		result, err := tracker.ViewUsage(from, to)
		if err != nil {
			t.Fatal("Failed to view usage:", err)
		}

		// Verify results
		t.Logf("Usage results: %+v", result)

		// Both validators should appear in results
		validator1Usage, validator1Exists := result[validators[0]]
		validator2Usage, validator2Exists := result[validators[9]]

		if !validator1Exists {
			t.Error("Validator1 not found in results")
		}

		if !validator2Exists {
			t.Error("Validator2 not found in results")
		}

		// Validator1 should have 2 time buckets (recorded twice)
		if validator1Usage != 10*time.Minute {
			t.Errorf("Expected validator1 to have 2 usage records, got %d", validator1Usage)
		}

		// Validator2 should have 1 time bucket (recorded once)
		if validator2Usage != 5*time.Minute {
			t.Errorf("Expected validator2 to have 1 usage record, got %d", validator2Usage)
		}

		t.Log("Test passed: usage tracking works")
	})
}

func TestSQLiteUsageTrackerQuantization(t *testing.T) {
	tracker, cleanup, err := setupSQLiteTestDatabase(t, 2*time.Second)
	if err != nil {
		t.Fatal("Failed to set up test database:", err)
	}
	defer cleanup()

	validator := types.ValidatorPubkey{0x01, 0x02, 0x03}
	validators := []string{validator.Hex()}

	// Record usage multiple times within same quantization window
	usageCount := 3
	for i := 0; i < usageCount; i++ {
		time.Sleep(1 * time.Second)
		err = tracker.RecordUsage(validators)
		if err != nil {
			t.Fatal("Failed to record usage:", err)
		}
	}

	// View usage
	now := time.Now()
	from := now.Add(-3 * time.Minute)
	to := now.Add(1 * time.Minute)

	result, err := tracker.ViewUsage(from, to)
	if err != nil {
		t.Fatal("Failed to view usage:", err)
	}

	validatorKey := validator.Hex()
	usage, exists := result[validatorKey]
	if !exists {
		t.Fatal("Validator not found in results")
	}

	// Should have exactly 2 time buckets (first recording + one more after 2 seconds)
	expectedDuration := 2 * 2 * time.Second // 2 buckets * 2-second precision
	if usage != expectedDuration {
		t.Fatalf("Expected %v total usage, got %v", expectedDuration, usage)
	}

	t.Log("Test passed: quantization works correctly")
}

func TestSQLiteUsageTrackerEmptyRange(t *testing.T) {
	tracker, cleanup, err := setupSQLiteTestDatabase(t, 5*time.Minute)
	if err != nil {
		t.Fatal("Failed to set up test database:", err)
	}
	defer cleanup()

	// View usage on empty database
	now := time.Now()
	from := now.Add(-1 * time.Hour)
	to := now.Add(-30 * time.Minute)

	result, err := tracker.ViewUsage(from, to)
	if err != nil {
		t.Fatal("Failed to view usage:", err)
	}

	if len(result) != 0 {
		t.Fatalf("Expected empty result, got %d entries", len(result))
	}

	t.Log("Test passed: empty range returns empty result")
}

func setupSQLiteTestDatabase(t *testing.T, precision time.Duration) (UsageTracker, func(), error) {
	logger := zaptest.NewLogger(t)

	db, err := sql.Open("sqlite3", "file:test.db?mode=memory")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}

	tracker := &SQLiteUsageTracker{
		Database:  db,
		Logger:    logger,
		Precision: precision,
	}

	if err := tracker.initSchema(); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	cleanup := func() {
		tracker.Close()
	}

	return tracker, cleanup, nil
}
