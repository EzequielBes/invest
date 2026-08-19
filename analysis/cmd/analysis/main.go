// analysis/cmd/analysis/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	riskstorage "risk-engine/storage"

	"analysis/internal/llm"
	"analysis/internal/storage"
	"analysis/runner"
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
	runID, successCount, err := runner.Run(ctx, store, riskStore, llm.NewAnthropicClient(), assets, assetNames, timeframe, requestedAgents)
	if err != nil {
		return err
	}
	fmt.Printf("analysis run %s completed (%d narratives generated)\n", runID, successCount)
	return nil
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
