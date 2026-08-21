package metrics

import (
	"fmt"
	"math"
	"time"
)

// EquityPoint is one chronological observation from an equity curve.
type EquityPoint struct {
	Time   time.Time
	Equity float64
}

// EquitySummary describes drawdown and recovery behavior of an equity curve.
// Durations are zero when the corresponding kind of episode is absent.
type EquitySummary struct {
	MaxDrawdownPct        float64
	CurrentDrawdownPct    float64
	MaxRecoveryDuration   time.Duration
	CurrentTimeUnderWater time.Duration
	TimeUnderWater        time.Duration
}

// EquityMetrics calculates drawdown and recovery metrics for chronological,
// positive equity observations. Recovered episodes contribute to
// TimeUnderWater; an episode still below its high-water mark is reported
// separately by CurrentTimeUnderWater.
func EquityMetrics(points []EquityPoint) (EquitySummary, error) {
	if len(points) == 0 {
		return EquitySummary{}, fmt.Errorf("equity curve is empty")
	}

	peak := points[0]
	if err := validateEquityPoint(peak); err != nil {
		return EquitySummary{}, err
	}

	var summary EquitySummary
	underwater := false
	for i := 1; i < len(points); i++ {
		point := points[i]
		if err := validateEquityPoint(point); err != nil {
			return EquitySummary{}, err
		}
		if point.Time.Before(points[i-1].Time) {
			return EquitySummary{}, fmt.Errorf("equity points are not chronological")
		}

		if point.Equity >= peak.Equity {
			if underwater {
				duration := point.Time.Sub(peak.Time)
				summary.TimeUnderWater += duration
				if duration > summary.MaxRecoveryDuration {
					summary.MaxRecoveryDuration = duration
				}
				underwater = false
			}
			if point.Equity > peak.Equity {
				peak = point
			}
			continue
		}

		drawdown := (peak.Equity - point.Equity) / peak.Equity * 100
		if drawdown > summary.MaxDrawdownPct {
			summary.MaxDrawdownPct = drawdown
		}
		underwater = true
	}

	last := points[len(points)-1]
	summary.CurrentDrawdownPct = (peak.Equity - last.Equity) / peak.Equity * 100
	if underwater {
		summary.CurrentTimeUnderWater = last.Time.Sub(peak.Time)
	}
	return summary, nil
}

func validateEquityPoint(point EquityPoint) error {
	if point.Time.IsZero() {
		return fmt.Errorf("equity point time is required")
	}
	if point.Equity <= 0 || math.IsNaN(point.Equity) || math.IsInf(point.Equity, 0) {
		return fmt.Errorf("equity must be positive and finite")
	}
	return nil
}
