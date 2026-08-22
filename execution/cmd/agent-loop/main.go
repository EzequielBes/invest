// agent-loop runs the configured external MCP client without embedding MCP or
// exchange credentials in the scheduler itself.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"analysis/runner"
	"execution/paperstore"
)

const promptTemplate = `You are the investment platform's specialist in asset-universe governance and risk-aware research. Use the configured investment-platform MCP server only; do not call exchange, database, HTTP, or other APIs directly. The asset universe is %q (comma-separated symbols).

First call get_automation_controls. If both paper_enabled and testnet_enabled are false, stop without changing anything. Otherwise run exactly this staged workflow: call prepare_analysis exactly once with the asset universe above and timeframe %q. Its exclusions are deterministic policy failures: do not analyze, rank, or create intents for excluded assets. If it returns no analysis_run_id, all candidates were excluded: report the exclusions and stop. For each eligible asset, assess data quality, liquidity, market structure, derivatives, and material protocol/news risks; compare evidence, not price predictions. Use its analysis_run_id to call get_analysis_context; submit_analysis_narratives in order for technical, derivatives, news, risk_context, then macro. For every risk_context narrative, set asset to the empty string. For every committee assessment, thesis must be exactly one of bull|bear|neutro; do not use English alternatives. The narrative must state the evidence and unresolved risks supporting the assessment. Call submit_committee_assessments; call prepare_strategy; then call apply_strategy_intents with only explicit targets currently enabled by get_automation_controls. Never call prepare_analysis again in this cycle. Never change automation controls. Do not use legacy run_analysis, run_strategist, or run_paper_strategist tools.`

func prompt(assets, timeframe string) string { return fmt.Sprintf(promptTemplate, assets, timeframe) }

const stagedMCPTools = "mcp__investment-platform__get_automation_controls," +
	"mcp__investment-platform__prepare_analysis," +
	"mcp__investment-platform__get_analysis_context," +
	"mcp__investment-platform__submit_analysis_narratives," +
	"mcp__investment-platform__submit_committee_assessments," +
	"mcp__investment-platform__prepare_strategy," +
	"mcp__investment-platform__apply_strategy_intents"

type controlsReader interface {
	GetAutomationControls(context.Context) (paperstore.AutomationControls, error)
}

type commandRunner func(context.Context, string, ...string) error

type loop struct {
	controls controlsReader
	cleanup  func(context.Context) error
	run      commandRunner
	claude   string
	codex    string
	prompt   string
}

func (l loop) cycle(ctx context.Context) error {
	controls, err := l.controls.GetAutomationControls(ctx)
	if err != nil {
		return fmt.Errorf("get automation controls: %w", err)
	}
	if !controls.Enabled && !controls.TestnetEnabled {
		return nil
	}
	if l.cleanup != nil {
		if err := l.cleanup(ctx); err != nil {
			return fmt.Errorf("cleanup stale analysis runs: %w", err)
		}
	}

	switch controls.ActiveAgent {
	case "claude_code":
		if l.claude == "" {
			return errors.New("CLAUDE_CODE_BIN is required when active_agent is claude_code")
		}
		return l.run(ctx, l.claude, "-p", l.prompt, "--allowedTools", stagedMCPTools)
	case "codex":
		if l.codex == "" {
			return errors.New("CODEX_BIN is required when active_agent is codex")
		}
		return l.run(ctx, l.codex, "exec", l.prompt)
	default:
		return fmt.Errorf("unsupported active agent %q", controls.ActiveAgent)
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	assets := os.Getenv("AUTOMATION_ASSETS")
	if assets == "" {
		return errors.New("AUTOMATION_ASSETS is required")
	}
	timeframe, err := automationTimeframe(os.Getenv("AUTOMATION_TIMEFRAME"))
	if err != nil {
		return err
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	interval, err := durationEnv("AGENT_LOOP_INTERVAL", 15*time.Minute)
	if err != nil {
		return err
	}
	timeout, err := durationEnv("AGENT_TIMEOUT", 10*time.Minute)
	if err != nil {
		return err
	}

	unlock, err := lock(filepath.Join(os.TempDir(), "invest-agent-loop.lock"))
	if err != nil {
		return err
	}
	defer unlock()

	store, err := paperstore.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect paper storage: %w", err)
	}
	defer store.Close()

	l := loop{controls: store, cleanup: func(ctx context.Context) error {
		return runner.CleanupStalePendingWithDSN(ctx, dsn)
	}, claude: os.Getenv("CLAUDE_CODE_BIN"), codex: os.Getenv("CODEX_BIN"), prompt: prompt(assets, timeframe), run: func(ctx context.Context, name string, args ...string) error {
		return exec.CommandContext(ctx, name, args...).Run()
	}}
	for {
		cycleCtx, cancel := context.WithTimeout(ctx, timeout)
		err := l.cycle(cycleCtx)
		cancel()
		if err != nil {
			log.Printf("agent cycle: %v", err)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
	}
}

func automationTimeframe(value string) (string, error) {
	if value == "" {
		return "1h", nil
	}
	if strings.TrimSpace(value) == "" {
		return "", errors.New("AUTOMATION_TIMEFRAME must not be empty")
	}
	return value, nil
}

func durationEnv(name string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return duration, nil
}

func lock(path string) (func(), error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open loop lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("agent loop is already running: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}
