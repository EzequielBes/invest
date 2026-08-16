package risk

import "testing"

func TestCheckDailyLoss(t *testing.T) {
	over := PortfolioState{DailyLoss: 0.08}
	if r := checkDailyLoss(over, 0.05); r.Passed {
		t.Fatalf("expected rejection: daily loss 0.08 > limit 0.05")
	}

	under := PortfolioState{DailyLoss: 0.02}
	if r := checkDailyLoss(under, 0.05); !r.Passed {
		t.Fatalf("expected approval: daily loss 0.02 <= limit 0.05")
	}
}

func TestCheckWeeklyLoss(t *testing.T) {
	over := PortfolioState{WeeklyLoss: 0.15}
	if r := checkWeeklyLoss(over, 0.10); r.Passed {
		t.Fatalf("expected rejection: weekly loss 0.15 > limit 0.10")
	}
}

func TestCheckDrawdown(t *testing.T) {
	over := PortfolioState{Drawdown: 0.25}
	if r := checkDrawdown(over, 0.20); r.Passed {
		t.Fatalf("expected rejection: drawdown 0.25 > limit 0.20")
	}
}

func TestCheckConsecutiveLosses(t *testing.T) {
	over := PortfolioState{ConsecutiveLosses: 6}
	if r := checkConsecutiveLosses(over, 5); r.Passed {
		t.Fatalf("expected rejection: 6 consecutive losses > limit 5")
	}

	under := PortfolioState{ConsecutiveLosses: 3}
	if r := checkConsecutiveLosses(under, 5); !r.Passed {
		t.Fatalf("expected approval: 3 consecutive losses <= limit 5")
	}
}
