// simulation/internal/engine/clock.go
package engine

import "time"

// Clock advances in fixed increments of duration (the driving timeframe)
// from Start to End. Next reports each step's [openTime, closeTime)
// candle boundary until closeTime would exceed End.
type Clock struct {
	current  time.Time
	end      time.Time
	duration time.Duration
}

func NewClock(start, end time.Time, duration time.Duration) *Clock {
	return &Clock{current: start, end: end, duration: duration}
}

// Next returns the next candle's [openTime, closeTime) and advances the
// clock. ok is false once closeTime would exceed End — the run is done.
func (c *Clock) Next() (openTime, closeTime time.Time, ok bool) {
	closeTime = c.current.Add(c.duration)
	if closeTime.After(c.end) {
		return time.Time{}, time.Time{}, false
	}
	openTime = c.current
	c.current = closeTime
	return openTime, closeTime, true
}
