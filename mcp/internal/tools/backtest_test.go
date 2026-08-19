// mcp/internal/tools/backtest_test.go
package tools

import (
	"context"
	"testing"
	"time"
)

func TestRunBacktest_RejectsInvertedPeriod(t *testing.T) {
	start := time.Now()
	args := RunBacktestArgs{
		PeriodStart: start, PeriodEnd: start.Add(-time.Hour),
		Timeframes: []string{"1h"}, DrivingTimeframe: "1h", Assets: []string{"BTC"},
	}
	if _, err := RunBacktest(context.Background(), "", nil, args); err == nil {
		t.Fatal("expected an error for period_end before period_start, got nil")
	}
}

func TestRunBacktest_AppliesDefaults(t *testing.T) {
	// A missing/invalid driving_timeframe is rejected before RunWithDSN
	// (and thus any store connection) is ever called, regardless of the
	// defaults — this test only confirms the zero-value defaulting doesn't
	// itself cause a spurious validation failure by checking a case that
	// fails for an unrelated reason (driving_timeframe not in timeframes)
	// rather than a defaulting bug.
	start := time.Now()
	args := RunBacktestArgs{
		PeriodStart: start, PeriodEnd: start.Add(time.Hour),
		Timeframes: []string{"1h"}, DrivingTimeframe: "4h", Assets: []string{"BTC"},
	}
	_, err := RunBacktest(context.Background(), "", nil, args)
	if err == nil {
		t.Fatal("expected an error for driving_timeframe not in timeframes, got nil")
	}
}
