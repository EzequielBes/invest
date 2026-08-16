package risk

import "fmt"

func checkDailyLoss(portfolio PortfolioState, maxDailyLoss float64) RuleResult {
	return RuleResult{
		Rule: "daily_loss", Passed: portfolio.DailyLoss <= maxDailyLoss,
		Measured: portfolio.DailyLoss, Limit: maxDailyLoss,
		Detail: fmt.Sprintf("daily loss so far: %.4f", portfolio.DailyLoss),
	}
}

func checkWeeklyLoss(portfolio PortfolioState, maxWeeklyLoss float64) RuleResult {
	return RuleResult{
		Rule: "weekly_loss", Passed: portfolio.WeeklyLoss <= maxWeeklyLoss,
		Measured: portfolio.WeeklyLoss, Limit: maxWeeklyLoss,
		Detail: fmt.Sprintf("weekly loss so far: %.4f", portfolio.WeeklyLoss),
	}
}

func checkDrawdown(portfolio PortfolioState, maxDrawdown float64) RuleResult {
	return RuleResult{
		Rule: "drawdown", Passed: portfolio.Drawdown <= maxDrawdown,
		Measured: portfolio.Drawdown, Limit: maxDrawdown,
		Detail: fmt.Sprintf("current drawdown: %.4f", portfolio.Drawdown),
	}
}

func checkConsecutiveLosses(portfolio PortfolioState, maxConsecutiveLosses int) RuleResult {
	return RuleResult{
		Rule: "consecutive_losses", Passed: portfolio.ConsecutiveLosses <= maxConsecutiveLosses,
		Measured: float64(portfolio.ConsecutiveLosses), Limit: float64(maxConsecutiveLosses),
		Detail: fmt.Sprintf("%d consecutive losses", portfolio.ConsecutiveLosses),
	}
}
