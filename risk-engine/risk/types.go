package risk

// Position is one held asset's current quantity and value, as reported by
// the caller — this module never persists portfolio state itself.
type Position struct {
	Asset    string
	Quantity float64
	Value    float64
}

// PortfolioState is supplied by the caller on every Evaluate call — the
// risk engine has no memory of it between calls.
type PortfolioState struct {
	Positions         map[string]Position
	Cash              float64
	DailyLoss         float64
	WeeklyLoss        float64
	Drawdown          float64
	ConsecutiveLosses int
}

type Side string

const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

type ProposedOperation struct {
	Asset    string
	Side     Side
	Quantity float64
	Value    float64
}

// RuleResult is one limit's evaluation, kept regardless of pass/fail so
// every decision is fully auditable.
type RuleResult struct {
	Rule     string
	Passed   bool
	Measured float64
	Limit    float64
	Detail   string
}

type Decision struct {
	Allowed      bool
	Reasons      []string
	RulesChecked []RuleResult
}
