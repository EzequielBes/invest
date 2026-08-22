package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"risk-engine/risk"
	riskstorage "risk-engine/storage"

	analysisrunner "analysis/runner"
	"execution/executor"
	"execution/paperexec"
	"execution/paperstore"
	strategistrunner "strategist/runner"
)

type PrepareAnalysisArgs struct {
	Assets     []string          `json:"assets" jsonschema:"asset symbols to analyze"`
	Timeframe  string            `json:"timeframe,omitempty" jsonschema:"technical-analysis timeframe, defaults to 1h"`
	AssetNames map[string]string `json:"asset_names,omitempty" jsonschema:"optional symbol-to-name mapping for news context"`
}

type AnalysisContextItem struct {
	AgentType  string `json:"agent_type"`
	Asset      string `json:"asset"`
	Indicators any    `json:"indicators"`
}

type PrepareAnalysisResult struct {
	AnalysisRunID string                   `json:"analysis_run_id"`
	Context       []AnalysisContextItem    `json:"context"`
	Exclusions    []risk.EligibilityResult `json:"exclusions,omitempty"`
}

func PrepareAnalysis(ctx context.Context, dsn string, riskStore *riskstorage.Store, args PrepareAnalysisArgs) (PrepareAnalysisResult, error) {
	if len(args.Assets) == 0 {
		return PrepareAnalysisResult{}, fmt.Errorf("assets is required")
	}
	timeframe := args.Timeframe
	if timeframe == "" {
		timeframe = "1h"
	}
	prepared, err := analysisrunner.PrepareWithDSN(ctx, dsn, riskStore, analysisrunner.PrepareRequest{Assets: args.Assets, AssetNames: args.AssetNames, Timeframe: timeframe})
	if err != nil {
		return PrepareAnalysisResult{}, err
	}
	result := PrepareAnalysisResult{AnalysisRunID: prepared.RunID, Context: make([]AnalysisContextItem, len(prepared.Context)), Exclusions: append([]risk.EligibilityResult{}, prepared.Exclusions...)}
	for i, item := range prepared.Context {
		indicators, err := decodeIndicators(item.Indicators)
		if err != nil {
			return PrepareAnalysisResult{}, fmt.Errorf("decode indicators for %s/%s: %w", item.AgentType, item.Asset, err)
		}
		result.Context[i] = AnalysisContextItem{AgentType: item.AgentType, Asset: item.Asset, Indicators: indicators}
	}
	return result, nil
}

type GetAnalysisContextArgs struct {
	AnalysisRunID string `json:"analysis_run_id" jsonschema:"pending analysis run ID from prepare_analysis"`
}

func GetAnalysisContext(ctx context.Context, dsn string, args GetAnalysisContextArgs) (PrepareAnalysisResult, error) {
	if strings.TrimSpace(args.AnalysisRunID) == "" {
		return PrepareAnalysisResult{}, fmt.Errorf("analysis_run_id is required")
	}
	prepared, err := analysisrunner.GetContextWithDSN(ctx, dsn, args.AnalysisRunID)
	if err != nil {
		return PrepareAnalysisResult{}, err
	}
	result := PrepareAnalysisResult{AnalysisRunID: prepared.RunID, Context: make([]AnalysisContextItem, len(prepared.Context)), Exclusions: []risk.EligibilityResult{}}
	for i, item := range prepared.Context {
		indicators, err := decodeIndicators(item.Indicators)
		if err != nil {
			return PrepareAnalysisResult{}, fmt.Errorf("decode indicators for %s/%s: %w", item.AgentType, item.Asset, err)
		}
		result.Context[i] = AnalysisContextItem{AgentType: item.AgentType, Asset: item.Asset, Indicators: indicators}
	}
	return result, nil
}

func decodeIndicators(raw json.RawMessage) (any, error) {
	var indicators any
	if err := json.Unmarshal(raw, &indicators); err != nil {
		return nil, err
	}
	return indicators, nil
}

type NarrativeSubmission struct {
	Asset     string `json:"asset" jsonschema:"asset symbol; for risk_context, must be the empty string"`
	Narrative string `json:"narrative"`
}

type SubmitAnalysisNarrativesArgs struct {
	AnalysisRunID string                `json:"analysis_run_id" jsonschema:"pending analysis run ID from prepare_analysis"`
	Stage         string                `json:"stage" jsonschema:"one of technical, derivatives, news, risk_context, macro"`
	Narratives    []NarrativeSubmission `json:"narratives"`
}

type SubmissionResult struct {
	Submitted bool `json:"submitted"`
}

func SubmitAnalysisNarratives(ctx context.Context, dsn string, args SubmitAnalysisNarrativesArgs) (SubmissionResult, error) {
	if strings.TrimSpace(args.AnalysisRunID) == "" {
		return SubmissionResult{}, fmt.Errorf("analysis_run_id is required")
	}
	if args.Stage != "technical" && args.Stage != "derivatives" && args.Stage != "news" && args.Stage != "risk_context" && args.Stage != "macro" {
		return SubmissionResult{}, fmt.Errorf("invalid narrative stage %q", args.Stage)
	}
	narratives := make([]analysisrunner.NarrativeSubmission, len(args.Narratives))
	for i, narrative := range args.Narratives {
		narratives[i] = analysisrunner.NarrativeSubmission(narrative)
	}
	if err := analysisrunner.SubmitNarrativesWithDSN(ctx, dsn, args.AnalysisRunID, args.Stage, narratives); err != nil {
		return SubmissionResult{}, err
	}
	return SubmissionResult{Submitted: true}, nil
}

type CommitteeAssessment struct {
	Asset            string                    `json:"asset"`
	Thesis           string                    `json:"thesis" jsonschema:"exact accepted enum: bull|bear|neutro"`
	Confidence       float64                   `json:"confidence"`
	OpportunityScore float64                   `json:"opportunity_score"`
	Narrative        string                    `json:"narrative"`
	Evidence         []analysisrunner.Evidence `json:"evidence"`
}

type SubmitCommitteeAssessmentsArgs struct {
	AnalysisRunID string                `json:"analysis_run_id" jsonschema:"pending analysis run ID after the macro narrative is submitted"`
	Assessments   []CommitteeAssessment `json:"assessments"`
}

func SubmitCommitteeAssessments(ctx context.Context, dsn string, riskStore *riskstorage.Store, args SubmitCommitteeAssessmentsArgs) (SubmissionResult, error) {
	if strings.TrimSpace(args.AnalysisRunID) == "" {
		return SubmissionResult{}, fmt.Errorf("analysis_run_id is required")
	}
	assessments := make([]analysisrunner.CommitteeAssessment, len(args.Assessments))
	for i, assessment := range args.Assessments {
		assessments[i] = analysisrunner.CommitteeAssessment(assessment)
	}
	if err := analysisrunner.CompleteWithCommitteeWithDSN(ctx, dsn, riskStore, args.AnalysisRunID, assessments); err != nil {
		return SubmissionResult{}, err
	}
	return SubmissionResult{Submitted: true}, nil
}

type PrepareStrategyArgs struct {
	AnalysisRunID string `json:"analysis_run_id" jsonschema:"completed analysis run ID"`
}

type StrategyRanking struct {
	Asset          string  `json:"asset"`
	Rank           int     `json:"rank"`
	CompositeScore float64 `json:"composite_score"`
	Thesis         string  `json:"thesis"`
	Confidence     float64 `json:"confidence"`
}

type PrepareStrategyResult struct {
	AnalysisRunID string            `json:"analysis_run_id"`
	Rankings      []StrategyRanking `json:"rankings"`
}

func PrepareStrategy(ctx context.Context, dsn string, args PrepareStrategyArgs) (PrepareStrategyResult, error) {
	if strings.TrimSpace(args.AnalysisRunID) == "" {
		return PrepareStrategyResult{}, fmt.Errorf("analysis_run_id is required")
	}
	strategy, err := strategistrunner.PrepareWithDSN(ctx, dsn, args.AnalysisRunID)
	result := PrepareStrategyResult{AnalysisRunID: strategy.AnalysisRunID, Rankings: make([]StrategyRanking, len(strategy.Rankings))}
	for i, ranking := range strategy.Rankings {
		result.Rankings[i] = StrategyRanking{Asset: ranking.Asset, Rank: ranking.Rank, CompositeScore: ranking.CompositeScore, Thesis: ranking.Thesis, Confidence: ranking.Confidence}
	}
	return result, err
}

type StrategyIntent = strategistrunner.Intent

type ApplyStrategyIntentsArgs struct {
	AnalysisRunID string           `json:"analysis_run_id" jsonschema:"completed analysis run ID"`
	Intents       []StrategyIntent `json:"intents"`
	Targets       []string         `json:"targets" jsonschema:"explicit execution targets: paper, testnet, and/or alpaca_paper; real trading is not supported"`
}

type StrategyApplication struct {
	IntentID         string   `json:"intent_id"`
	TargetID         string   `json:"target_id"`
	Asset            string   `json:"asset"`
	Side             string   `json:"side"`
	Quantity         float64  `json:"quantity"`
	Value            float64  `json:"value"`
	RiskAllowed      *bool    `json:"risk_allowed,omitempty"`
	RiskReasons      []string `json:"risk_reasons,omitempty"`
	ExecutionStatus  string   `json:"execution_status"`
	ExecutionOrderID string   `json:"execution_order_id,omitempty"`
}

type ApplyStrategyIntentsResult struct {
	Applications []StrategyApplication `json:"applications"`
}

func ApplyStrategyIntents(ctx context.Context, dsn string, riskStore *riskstorage.Store, paperStore *paperstore.Store, args ApplyStrategyIntentsArgs) (ApplyStrategyIntentsResult, error) {
	if strings.TrimSpace(args.AnalysisRunID) == "" {
		return ApplyStrategyIntentsResult{}, fmt.Errorf("analysis_run_id is required")
	}
	requested, err := requestedTargets(args.Targets)
	if err != nil {
		return ApplyStrategyIntentsResult{}, err
	}
	controls, err := paperStore.GetAutomationControls(ctx)
	if err != nil {
		return ApplyStrategyIntentsResult{}, fmt.Errorf("get automation controls: %w", err)
	}
	if requested["paper"] && !controls.Enabled {
		return ApplyStrategyIntentsResult{}, fmt.Errorf("paper automation is disabled")
	}
	if requested["testnet"] && !controls.TestnetEnabled {
		return ApplyStrategyIntentsResult{}, fmt.Errorf("testnet automation is disabled")
	}
	if requested["alpaca_paper"] && !controls.AlpacaPaperEnabled {
		return ApplyStrategyIntentsResult{}, fmt.Errorf("alpaca paper automation is disabled")
	}

	targets := make([]strategistrunner.Target, 0, len(args.Targets))
	closers := make([]func(), 0, len(args.Targets))
	defer func() {
		for _, close := range closers {
			close()
		}
	}()
	if requested["paper"] {
		metrics, err := paperStore.MarkToMarket(ctx)
		if err != nil {
			return ApplyStrategyIntentsResult{}, fmt.Errorf("mark paper portfolio: %w", err)
		}
		client, err := paperexec.New(ctx, dsn)
		if err != nil {
			return ApplyStrategyIntentsResult{}, err
		}
		closers = append(closers, client.Close)
		targets = append(targets, strategistrunner.Target{ID: "paper", Executor: client,
			DailyLoss: metrics.DailyLoss, WeeklyLoss: metrics.WeeklyLoss, Drawdown: metrics.Drawdown, ConsecutiveLosses: metrics.ConsecutiveLosses})
	}
	if requested["testnet"] {
		client, err := executor.NewClient(ctx, dsn)
		if err != nil {
			return ApplyStrategyIntentsResult{}, err
		}
		closers = append(closers, client.Close)
		targets = append(targets, strategistrunner.Target{ID: "testnet", Executor: client})
	}
	if requested["alpaca_paper"] {
		client, err := executor.NewAlpacaClient(ctx, dsn)
		if err != nil {
			return ApplyStrategyIntentsResult{}, err
		}
		closers = append(closers, client.Close)
		targets = append(targets, strategistrunner.Target{ID: "alpaca_paper", Executor: client})
	}

	applications, err := strategistrunner.ApplyIntentsWithDSN(ctx, dsn, riskStore, strategistrunner.StrategyContext{AnalysisRunID: args.AnalysisRunID}, args.Intents, targets)
	if err != nil {
		return ApplyStrategyIntentsResult{}, err
	}
	result := ApplyStrategyIntentsResult{Applications: make([]StrategyApplication, len(applications))}
	for i, application := range applications {
		result.Applications[i] = StrategyApplication{IntentID: application.IntentID, TargetID: application.TargetID, Asset: application.Asset, Side: application.Side, Quantity: application.Quantity, Value: application.Value, RiskAllowed: application.RiskAllowed, RiskReasons: application.RiskReasons, ExecutionStatus: application.ExecutionStatus, ExecutionOrderID: application.ExecutionOrderID}
	}
	return result, nil
}

func requestedTargets(targets []string) (map[string]bool, error) {
	if len(targets) == 0 {
		return nil, fmt.Errorf("at least one explicit target is required")
	}
	requested := make(map[string]bool, len(targets))
	for _, target := range targets {
		if (target != "paper" && target != "testnet" && target != "alpaca_paper") || requested[target] {
			return nil, fmt.Errorf("invalid target %q (valid: paper, testnet, alpaca_paper)", target)
		}
		requested[target] = true
	}
	return requested, nil
}

type GetAutomationControlsArgs struct{}

type AutomationControlsResult struct {
	PaperEnabled       bool   `json:"paper_enabled"`
	TestnetEnabled     bool   `json:"testnet_enabled"`
	AlpacaPaperEnabled bool   `json:"alpaca_paper_enabled"`
	ActiveAgent        string `json:"active_agent"`
}

func GetAutomationControls(ctx context.Context, store *paperstore.Store) (AutomationControlsResult, error) {
	controls, err := store.GetAutomationControls(ctx)
	return AutomationControlsResult{PaperEnabled: controls.Enabled, TestnetEnabled: controls.TestnetEnabled, AlpacaPaperEnabled: controls.AlpacaPaperEnabled, ActiveAgent: controls.ActiveAgent}, err
}

type SetAutomationControlsArgs struct {
	PaperEnabled       *bool   `json:"paper_enabled,omitempty" jsonschema:"optional paper automation switch"`
	TestnetEnabled     *bool   `json:"testnet_enabled,omitempty" jsonschema:"optional testnet automation switch"`
	AlpacaPaperEnabled *bool   `json:"alpaca_paper_enabled,omitempty" jsonschema:"optional alpaca stock paper automation switch"`
	ActiveAgent        *string `json:"active_agent,omitempty" jsonschema:"optional external automation agent: claude_code or codex"`
}

func SetAutomationControls(ctx context.Context, store *paperstore.Store, args SetAutomationControlsArgs) (AutomationControlsResult, error) {
	if args.PaperEnabled == nil && args.TestnetEnabled == nil && args.AlpacaPaperEnabled == nil && args.ActiveAgent == nil {
		return AutomationControlsResult{}, fmt.Errorf("at least one automation control is required")
	}
	controls, err := store.PatchAutomationControls(ctx, paperstore.AutomationPatch{Enabled: args.PaperEnabled, TestnetEnabled: args.TestnetEnabled, AlpacaPaperEnabled: args.AlpacaPaperEnabled, ActiveAgent: args.ActiveAgent})
	return AutomationControlsResult{PaperEnabled: controls.Enabled, TestnetEnabled: controls.TestnetEnabled, AlpacaPaperEnabled: controls.AlpacaPaperEnabled, ActiveAgent: controls.ActiveAgent}, err
}
