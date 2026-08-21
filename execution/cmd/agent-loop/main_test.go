package main

import (
	"context"
	"strings"
	"testing"

	"execution/paperstore"
)

type fakeControls struct {
	controls paperstore.AutomationControls
	err      error
}

func (f fakeControls) GetAutomationControls(context.Context) (paperstore.AutomationControls, error) {
	return f.controls, f.err
}

func TestCycle_DisabledDoesNotRunAgent(t *testing.T) {
	calls := 0
	l := loop{controls: fakeControls{}, cleanup: func(context.Context) error {
		calls++
		return nil
	}, run: func(context.Context, string, ...string) error {
		calls++
		return nil
	}}
	if err := l.cycle(context.Background()); err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if calls != 0 {
		t.Errorf("cleanup or agent calls = %d, want 0", calls)
	}
}

func TestCycle_RunsSelectedAgentWithFixedPrompt(t *testing.T) {
	tests := []struct {
		name  string
		agent string
		bin   string
		args  []string
	}{
		{name: "claude", agent: "claude_code", bin: "/opt/claude", args: []string{"-p", prompt("1h")}},
		{name: "codex", agent: "codex", bin: "/opt/codex", args: []string{"exec", prompt("1h")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotName string
			var gotArgs []string
			cleaned := false
			claude, codex := "", ""
			if tt.agent == "claude_code" {
				claude = tt.bin
			} else {
				codex = tt.bin
			}
			l := loop{
				controls: fakeControls{controls: paperstore.AutomationControls{Enabled: true, ActiveAgent: tt.agent}},
				cleanup: func(context.Context) error {
					cleaned = true
					return nil
				},
				claude: claude,
				codex:  codex,
				prompt: prompt("1h"),
				run: func(_ context.Context, name string, args ...string) error {
					if !cleaned {
						t.Fatal("agent ran before stale analysis cleanup")
					}
					gotName, gotArgs = name, args
					return nil
				},
			}
			if err := l.cycle(context.Background()); err != nil {
				t.Fatalf("cycle: %v", err)
			}
			if gotName != tt.bin || len(gotArgs) != len(tt.args) {
				t.Fatalf("command = %q %q, want %q %q", gotName, gotArgs, tt.bin, tt.args)
			}
			for i := range gotArgs {
				if gotArgs[i] != tt.args[i] {
					t.Errorf("arg %d = %q, want %q", i, gotArgs[i], tt.args[i])
				}
			}
		})
	}
}

func TestPrompt_IncludesTimeframeInPrepareAnalysis(t *testing.T) {
	if got := prompt("1m"); !strings.Contains(got, `prepare_analysis exactly once with AUTOMATION_ASSETS and timeframe "1m"`) {
		t.Errorf("prompt = %q, want prepare_analysis timeframe", got)
	}
}

func TestPrompt_GetsContextWithoutRepeatingPreparation(t *testing.T) {
	got := prompt("1h")
	if !strings.Contains(got, "prepare_analysis exactly once") || !strings.Contains(got, "get_analysis_context") || !strings.Contains(got, "Never call prepare_analysis again in this cycle") {
		t.Errorf("prompt = %q, want one preparation followed by context retrieval", got)
	}
}

func TestPrompt_StatesSubscriptionNarrativeConstraints(t *testing.T) {
	got := prompt("1h")
	for _, want := range []string{"risk_context narrative, set asset to the empty string", "thesis must be exactly one of bull|bear|neutro; do not use English alternatives", "specialist in asset-universe governance", "exclusions are deterministic policy failures", "all candidates were excluded"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt = %q, want %q", got, want)
		}
	}
}

func TestAutomationTimeframe(t *testing.T) {
	tests := []struct {
		value string
		want  string
		valid bool
	}{
		{value: "", want: "1h", valid: true},
		{value: "1m", want: "1m", valid: true},
		{value: "  ", valid: false},
	}
	for _, tt := range tests {
		got, err := automationTimeframe(tt.value)
		if (err == nil) != tt.valid || got != tt.want {
			t.Errorf("automationTimeframe(%q) = %q, %v; want %q, valid=%t", tt.value, got, err, tt.want, tt.valid)
		}
	}
}
