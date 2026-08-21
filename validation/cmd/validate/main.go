package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"validation/internal/audit"
	"validation/internal/storage"
	validation "validation/internal/validation"
)

func main() {
	hypothesisID := flag.String("hypothesis-id", "", "registered validation hypothesis ID")
	backtestRunID := flag.String("backtest-run-id", "", "simulation backtest run ID to audit")
	clientOrderID := flag.String("client-order-id", "", "execution client order ID to audit")
	splitsJSON := flag.String("splits-json", "", "JSON temporal splits with kind, start, end, and embargo_minutes")
	gitCommit := flag.String("commit", "", "optional Git commit associated with this audit")
	configJSON := flag.String("config-json", "", "JSON configuration recorded with this audit")
	flag.Parse()

	if *hypothesisID == "" || *configJSON == "" || *splitsJSON == "" || auditMode(*backtestRunID, *clientOrderID) == "" {
		flag.Usage()
		os.Exit(2)
	}
	var splits []validation.Split
	if err := json.Unmarshal([]byte(*splitsJSON), &splits); err != nil {
		log.Fatalf("parse splits-json: %v", err)
	}
	if err := validation.ValidateSplits(splits); err != nil {
		log.Fatalf("validate splits: %v", err)
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}

	ctx := context.Background()
	store, err := storage.New(ctx, dsn)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer store.Close()

	hypothesis, err := store.Hypothesis(ctx, *hypothesisID)
	if err != nil {
		log.Fatalf("load hypothesis: %v", err)
	}
	if err := storage.ValidateHypothesis(hypothesis); err != nil {
		log.Fatalf("validate hypothesis: %v", err)
	}
	if !json.Valid([]byte(*configJSON)) {
		log.Fatal("config-json must be valid JSON")
	}

	var run storage.Run
	if auditMode(*backtestRunID, *clientOrderID) == "backtest" {
		run, err = audit.Backtest(ctx, store, audit.BacktestInput{
			HypothesisID: *hypothesisID, BacktestRunID: *backtestRunID,
			Config: json.RawMessage(*configJSON), GitCommit: *gitCommit, Splits: splits,
		})
	} else {
		run, err = audit.Execution(ctx, store, audit.ExecutionInput{
			HypothesisID: *hypothesisID, ClientOrderID: *clientOrderID,
			Config: json.RawMessage(*configJSON), GitCommit: *gitCommit, Splits: splits,
		})
	}
	if err != nil {
		log.Fatalf("audit: %v", err)
	}
	findings, err := store.Findings(ctx, run.ID)
	if err != nil {
		log.Fatalf("load audit findings: %v", err)
	}

	fmt.Printf("validation_run_id=%s status=%s\n", run.ID, run.Status)
	for _, finding := range findings {
		fmt.Printf("finding severity=%s rule=%s message=%s\n", finding.Severity, finding.Rule, finding.Message)
	}
}

func auditMode(backtestRunID, clientOrderID string) string {
	if (backtestRunID == "") == (clientOrderID == "") {
		return ""
	}
	if backtestRunID != "" {
		return "backtest"
	}
	return "execution"
}
