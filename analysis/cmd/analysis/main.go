package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	riskstorage "risk-engine/storage"

	"analysis/internal/agents"
	"analysis/internal/llm"
	"analysis/internal/storage"
)

var validAgents = map[string]bool{"technical": true, "derivatives": true, "news": true, "risk_context": true}

func main() {
	assets := flag.String("assets", "", "comma-separated asset symbols on the reference exchange (required)")
	assetNames := flag.String("asset-names", "", "optional comma-separated SYMBOL=Full Name mappings used by the news agent")
	timeframe := flag.String("timeframe", "1h", "timeframe used by the technical agent")
	agentsFlag := flag.String("agents", "technical,derivatives,news,risk_context", "comma-separated agents to run")
	flag.Parse()
	if err := run(context.Background(), *assets, *assetNames, *timeframe, *agentsFlag); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, assetsStr, assetNamesStr, timeframe, agentsStr string) error {
	assets := splitNonEmpty(assetsStr)
	if len(assets) == 0 {
		return fmt.Errorf("-assets is required")
	}
	requestedAgents := splitNonEmpty(agentsStr)
	if len(requestedAgents) == 0 {
		return fmt.Errorf("-agents must not be empty")
	}
	for _, agentType := range requestedAgents {
		if !validAgents[agentType] {
			return fmt.Errorf("unknown agent %q (valid: technical, derivatives, news, risk_context)", agentType)
		}
	}
	assetNames, err := parseAssetNames(assetNamesStr)
	if err != nil {
		return err
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	store, err := storage.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect analysis storage: %w", err)
	}
	defer store.Close()
	riskStore, err := riskstorage.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect risk-engine storage: %w", err)
	}
	defer riskStore.Close()
	runID, successCount, err := runAnalysis(ctx, store, riskStore, llm.NewAnthropicClient(), assets, assetNames, timeframe, requestedAgents)
	if err != nil {
		return err
	}
	fmt.Printf("analysis run %s completed (%d narratives generated)\n", runID, successCount)
	return nil
}

// Run is exported so integration tests can exercise the orchestration with a fake LLM.
func Run(ctx context.Context, store *storage.Store, riskStore *riskstorage.Store, client llm.Client, assets []string, timeframe string, requestedAgents []string) (runID string, successCount int, err error) {
	return runAnalysis(ctx, store, riskStore, client, assets, nil, timeframe, requestedAgents)
}

func runAnalysis(ctx context.Context, store *storage.Store, riskStore *riskstorage.Store, client llm.Client, assets []string, assetNames map[string]string, timeframe string, requestedAgents []string) (runID string, successCount int, err error) {
	runID = uuid.NewString()
	if err := store.CreateRun(ctx, storage.Run{ID: runID, StartedAt: time.Now().UTC(), Timeframe: timeframe}); err != nil {
		return runID, 0, fmt.Errorf("create run: %w", err)
	}
	recordOne := func(agentType, asset string, out agents.Output, agentErr error) error {
		succeeded, err := record(ctx, store, runID, agentType, asset, out, agentErr)
		if succeeded {
			successCount++
		}
		return err
	}
	for _, agentType := range requestedAgents {
		switch agentType {
		case "technical":
			for _, asset := range assets {
				out, agentErr := agents.Technical(ctx, store, client, asset, timeframe)
				if err := recordOne(agentType, asset, out, agentErr); err != nil {
					return abortRun(ctx, store, runID, successCount, err)
				}
			}
		case "derivatives":
			for _, asset := range assets {
				out, agentErr := agents.Derivatives(ctx, store, client, asset)
				if err := recordOne(agentType, asset, out, agentErr); err != nil {
					return abortRun(ctx, store, runID, successCount, err)
				}
			}
		case "news":
			for _, asset := range assets {
				name := assetNames[asset]
				if name == "" {
					name = asset
				}
				out, agentErr := agents.News(ctx, store, client, asset, name)
				if err := recordOne(agentType, asset, out, agentErr); err != nil {
					return abortRun(ctx, store, runID, successCount, err)
				}
			}
		case "risk_context":
			out, agentErr := agents.RiskContext(ctx, riskStore, client)
			if err := recordOne(agentType, "", out, agentErr); err != nil {
				return abortRun(ctx, store, runID, successCount, err)
			}
		}
	}
	if successCount == 0 {
		return abortRun(ctx, store, runID, successCount, fmt.Errorf("all agents failed"))
	}
	if finishErr := store.FinishRun(ctx, runID, nil); finishErr != nil {
		return abortRun(ctx, store, runID, successCount, fmt.Errorf("finish run: %w", finishErr))
	}
	return runID, successCount, nil
}

type resultSaver interface {
	SaveResult(context.Context, storage.Result) error
}

func record(ctx context.Context, store resultSaver, runID, agentType, asset string, out agents.Output, agentErr error) (bool, error) {
	if agentErr != nil {
		fmt.Fprintf(os.Stderr, "%s/%s: data collection failed: %v\n", agentType, asset, agentErr)
		return false, nil
	}
	if err := store.SaveResult(ctx, storage.Result{ID: uuid.NewString(), RunID: runID, AgentType: agentType, Asset: asset, Indicators: out.Indicators, Narrative: out.Narrative, CreatedAt: time.Now().UTC()}); err != nil {
		return false, fmt.Errorf("save %s/%s result: %w", agentType, asset, err)
	}
	if out.Err != nil {
		fmt.Fprintf(os.Stderr, "%s/%s: sem narrativa: %v\n", agentType, asset, out.Err)
		return false, nil
	}
	fmt.Printf("%s/%s: %s\n", agentType, asset, out.Narrative)
	return true, nil
}

func abortRun(ctx context.Context, store *storage.Store, runID string, successCount int, runErr error) (string, int, error) {
	wrappedRunErr := fmt.Errorf("analysis run %s: %w", runID, runErr)
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if finishErr := store.FinishRun(cleanupCtx, runID, runErr); finishErr != nil {
		return runID, successCount, errors.Join(wrappedRunErr, fmt.Errorf("finish failed run: %w", finishErr))
	}
	return runID, successCount, wrappedRunErr
}

func splitNonEmpty(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parseAssetNames(value string) (map[string]string, error) {
	names := make(map[string]string)
	for _, mapping := range splitNonEmpty(value) {
		symbol, name, found := strings.Cut(mapping, "=")
		symbol = strings.TrimSpace(symbol)
		name = strings.TrimSpace(name)
		if !found || symbol == "" || name == "" {
			return nil, fmt.Errorf("invalid -asset-names entry %q (want SYMBOL=Full Name)", mapping)
		}
		names[symbol] = name
	}
	return names, nil
}
